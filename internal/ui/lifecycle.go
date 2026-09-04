package ui

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// deadSessionHint names both ways back from a dead row: revive resumes the
// conversation it held, restart drops it.
const deadSessionHint = "session is dead - press v to revive or R to restart"

// shellPromptHint refuses to write into a shell. SendText pastes and then
// presses Enter, so a sentence meant for an agent would run as a command
// on the user's machine. Entering the session is how text reaches a shell,
// where what is typed is plainly a command.
func shellPromptHint(name string) string {
	return name + " is a shell, not an agent - enter it to type there"
}

func (m *Model) attachSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok {
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	m.errBar.text = ""
	if err := m.store.AcknowledgeFinished(sess.ID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	return m, m.attachCmd(sess.ID)
}

// acknowledgeSelected marks the selected finished session idle and acked
// without entering it. Archived sessions keep their preserved status: the
// poller never re-derives it for them, so an ack would stick forever.
func (m *Model) acknowledgeSelected() (tea.Model, tea.Cmd) {
	sess, ok := m.selected()
	if !ok || sess.Archived || sess.Status != status.Finished {
		return m, nil
	}
	m.errBar.text = ""
	if err := m.store.AcknowledgeFinished(sess.ID); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.requestRefresh()
	return m, nil
}

func (m *Model) attachCmd(id string) tea.Cmd {
	// Flip the window back to auto-sizing so it fills the terminal on attach;
	// attachDoneMsg re-pins it to the preview width on detach. Clearing the
	// cached hash first keeps the poller from reading this reflow as
	// streaming output, same as the detach-side resize (reflowSessions).
	// A failure here still attaches: the worst outcome is a stale window
	// size, which beats locking the session out (issue #114).
	var prepErr error
	m.poller.reflowSessions([]string{id}, func() {
		prepErr = m.tmux.PrepareAttach(id)
	})
	if prepErr != nil {
		m.errBar.text = prepErr.Error()
	}
	return tea.ExecProcess(m.tmux.AttachCommand(id), func(err error) tea.Msg {
		return attachDoneMsg{sessID: id, err: err}
	})
}

func (m *Model) reattach(id string, diffGen int) tea.Cmd {
	driver := m.tmux
	stor := m.store
	poller := m.poller
	return func() tea.Msg {
		if !driver.Exists(id) {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: errors.New(deadSessionHint)}
		}
		sess, err := stor.Get(id)
		if err != nil {
			return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
		}
		if sess.Status == status.Finished {
			if err := stor.AcknowledgeFinished(sess.ID); err != nil {
				return reattachPreparedMsg{sessID: id, diffGen: diffGen, err: err}
			}
		}
		var prepErr error
		poller.reflowSessions([]string{id}, func() {
			prepErr = driver.PrepareAttach(id)
		})
		var warn string
		if prepErr != nil {
			warn = prepErr.Error()
		}
		return reattachPreparedMsg{sessID: id, diffGen: diffGen, warn: warn}
	}
}

// copyLastOutput copies the selected session's newest reply to the system
// clipboard, on demand: an explicit key rather than a finish-triggered
// write, which would silently overwrite whatever the user already had on
// the clipboard, and race several sessions finishing in a row for the
// same write. FullTurnText keeps every line of the turn, not just the
// last message_start block, so a reply with several marker-led
// paragraphs copies whole.
//
// This is bounded by whatever tmux capture-pane can see: the current
// screen, plus real scrollback for a tool that keeps any. An alt-screen,
// mouse-tracking tool (Claude Code, for one) keeps none of its own for
// tmux to walk, so a reply longer than the viewport truncates at
// whatever's on screen when y is pressed. deepCopyFallback covers that
// case for claude by reading the on-disk transcript instead, gated on
// the tail-match check documented there: AgentSessionID is captured
// once and can go stale (Claude Code opens a new transcript on its own
// compaction, invisibly to agent-manager), so the fallback is only
// trusted when its own tail provably continues what's already on
// screen, not just because the id is set.
func (m *Model) copyLastOutput() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup || m.engine == nil || m.tmux == nil {
		return m, nil
	}
	sess := entry.sess
	// quoteHistoryLines matches the poller's own row-quote capture depth:
	// past what's currently on screen when the tool keeps real tmux
	// scrollback to walk.
	pane, err := m.tmux.CapturePaneHistory(sess.ID, quoteHistoryLines)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	text, ok := m.engine.FullTurnText(sess.Tool, ansi.Strip(pane))
	if !ok || strings.TrimSpace(text) == "" {
		m.errBar.text = "nothing to copy"
		return m, nil
	}
	if deep, ok := m.deepCopyFallback(sess, text); ok {
		text = deep
	}
	if err := clipboard.WriteText(text); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.reportDone(fmt.Sprintf("copied %d chars from %s", len([]rune(text)), sess.Name))
	return m, nil
}

// staleCheckRunes is how much of the viewport-captured text has to show
// up, verbatim, at the end of the on-disk transcript's reply before
// deepCopyFallback trusts that reply. Long enough that an unrelated past
// conversation won't match it by coincidence, short enough to tolerate
// FullTurnText's own normalizing (collapsed blank lines, trimmed
// trailing spaces) not lining up byte-for-byte with the raw transcript
// text.
const staleCheckRunes = 80

// rescanTranscripts caps how many of the project's other transcripts
// deepCopyFallback checks once the stored AgentSessionID's own file
// fails the tail-match: newest first, most-likely-current one tried
// first, and a compacted session is rarely more than one or two rotations
// past the id agent-manager last captured.
const rescanTranscripts = 5

// deepCopyFallback returns the full reply from Claude Code's own on-disk
// transcript when the pane's captured viewport only got the tail of it -
// see copyLastOutput's doc comment for why that happens. viewportText is
// what copyLastOutput already has and trusts as current.
//
// AgentSessionID is captured once and never re-verified, and Claude Code
// opens a new transcript on its own context compaction, invisibly to
// agent-manager, leaving the stored id pointing at a real but unrelated
// past conversation - trusting it just because it's set would silently
// hand back the wrong task. So every candidate, the stored id included,
// is checked the same way: its transcript's reply has to provably
// continue viewportText, its own tail found inside the file's reply.
// Nothing here is used unless that holds.
//
// When the stored id fails but a newer transcript in the same project
// directory passes, that id replaces it in the store (SetAgentSessionID)
// so the next call - and the poller's own row-quote fallback, which
// trusts the stored id the same way - don't pay for the rescan again.
func (m *Model) deepCopyFallback(sess store.Session, viewportText string) (text string, ok bool) {
	if m.poller == nil || m.poller.mcpStyles[sess.Tool] != "claude" || sess.AgentSessionID == "" {
		return "", false
	}
	anchor := stripMarkdownPunct(tailRunes(strings.TrimSpace(viewportText), staleCheckRunes))
	if anchor == "" {
		return "", false
	}
	if reply, ok := claudeTranscriptReplyMatching(sess.Cwd, sess.AgentSessionID, anchor); ok {
		return reply, true
	}
	for _, id := range m.otherClaudeTranscripts(sess.Cwd, sess.AgentSessionID) {
		reply, ok := claudeTranscriptReplyMatching(sess.Cwd, id, anchor)
		if !ok {
			continue
		}
		if m.store != nil {
			_ = m.store.SetAgentSessionID(sess.ID, id)
		}
		return reply, true
	}
	return "", false
}

// claudeTranscriptReplyMatching reads sessionID's transcript tail and
// reports it only if its reply contains anchor - see deepCopyFallback.
// The comparison is on stripMarkdownPunct'd text on both sides: the raw
// transcript keeps Claude's own markdown (a code span's backticks, for
// one) that the terminal renders as styling rather than characters, so
// anchor - built from what actually painted on screen - would otherwise
// never match a reply that's really the same text.
func claudeTranscriptReplyMatching(cwd, sessionID, anchor string) (reply string, ok bool) {
	reply, tailOK := agentsession.ClaudeTranscriptFullTurn(cwd, sessionID)
	if !tailOK || strings.TrimSpace(reply) == "" || !strings.Contains(stripMarkdownPunct(reply), anchor) {
		return "", false
	}
	return reply, true
}

// otherClaudeTranscripts lists up to rescanTranscripts other session ids
// Claude Code has ever written a transcript for in cwd's project
// directory, newest first, skip is excluded (the id already tried).
func (m *Model) otherClaudeTranscripts(cwd, skip string) []string {
	dir, err := agentsession.ClaudeProjectDir(cwd)
	if err != nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type candidate struct {
		id      string
		modTime time.Time
	}
	candidates := make([]candidate, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		id := strings.TrimSuffix(name, ".jsonl")
		if id == name || id == skip {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{id: id, modTime: info.ModTime()})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})
	if len(candidates) > rescanTranscripts {
		candidates = candidates[:rescanTranscripts]
	}
	ids := make([]string, len(candidates))
	for i, c := range candidates {
		ids[i] = c.id
	}
	return ids
}

// tailRunes returns the last n runes of s, or the whole string if it has
// n or fewer.
func tailRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[len(runes)-n:])
}

// stripMarkdownPunct drops the characters Claude's own markdown uses for
// inline styling - a code span's backticks, emphasis's asterisks and
// underscores - that the terminal renders as color or weight rather
// than painting the character itself. Comparing text captured off
// screen against the raw transcript needs both sides free of them, or a
// styled word never matches its own unstyled-looking rendering.
func stripMarkdownPunct(s string) string {
	return strings.NewReplacer("`", "", "*", "", "_", "").Replace(s)
}

// reviveSelected relaunches a dead session's tmux session under the same
// id, keeping its name, group, and history. Tools with a revive_command
// resume where they left off (e.g. claude --continue). On a group row it
// revives the whole subtree, mirroring the group kill.
func (m *Model) reviveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		return m.reviveMany(m.sessionsInGroup(entry.group), "no dead sessions to revive in "+entry.group)
	}
	set, err := m.sessionAndChildren(entry.sess)
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	dead := false
	for _, sess := range set {
		if !m.tmux.Exists(sess.ID) {
			dead = true
			break
		}
	}
	if len(set) > 1 && dead {
		m.confirm = confirmTarget{
			action:   actionRevive,
			sessions: set,
			label: followConfirmLabel("revive", entry.sess.Name, len(set)-1,
				"brings it back.",
				"brings them back."),
		}
		m.mode = modeConfirmDelete
		return m, nil
	}
	// A pane the user quit the agent in is still a live window sitting at a
	// shell; the agent alone is gone, and it comes back inside that shell.
	if m.tmux.Exists(entry.sess.ID) {
		cmd, err := m.relaunchInPane(entry.sess)
		if err != nil {
			m.reportLaunchError(err)
			return m, nil
		}
		m.errBar.text = m.degradedResumeNotice(entry.sess)
		return m, cmd
	}
	if err := m.reviveSession(entry.sess); err != nil {
		m.reportLaunchError(err)
		return m, nil
	}
	m.errBar.text = m.degradedResumeNotice(entry.sess)
	m.requestRefresh()
	return m, nil
}

// reviveAllDead relaunches every dead session in the current view, resuming
// each by its captured id where one exists.
func (m *Model) reviveAllDead() (tea.Model, tea.Cmd) {
	return m.reviveMany(m.listedSessions(), "no dead sessions to revive")
}

// reviveMany relaunches every dead session in the list. It revives what it
// can and names the first failure rather than stopping, so one broken
// session does not block the rest.
func (m *Model) reviveMany(sessions []store.Session, emptyNotice string) (tea.Model, tea.Cmd) {
	revived, degraded := 0, 0
	var firstErr string
	for _, sess := range sessions {
		if sess.Status != status.Dead {
			continue
		}
		if err := m.reviveSession(sess); err != nil {
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		revived++
		if m.degradedResumeNotice(sess) != "" {
			degraded++
		}
	}
	switch {
	case revived == 0 && firstErr == "":
		m.errBar.text = emptyNotice
	case firstErr != "":
		m.errBar.text = fmt.Sprintf("revived %d, first error: %s", revived, firstErr)
	case degraded > 0:
		m.errBar.text = fmt.Sprintf("revived %d, %d without a captured id (used --continue)", revived, degraded)
	default:
		m.errBar.text = ""
	}
	m.requestRefresh()
	return m, nil
}

// sessionsInGroup lists the sessions the current view shows at or below a
// group, so a group action covers exactly the rows under it on screen.
func (m *Model) sessionsInGroup(path string) []store.Session {
	var sessions []store.Session
	for _, sess := range m.listedSessions() {
		if inGroupSubtree(sess.Group, path) {
			sessions = append(sessions, sess)
		}
	}
	return sessions
}

// degradedResumeNotice warns when a revived session had to fall back to the
// working directory's most recent conversation because its own conversation
// id was never captured, which resumes the wrong conversation whenever
// sessions share a directory.
func (m *Model) degradedResumeNotice(sess store.Session) string {
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok || sess.AgentSessionID != "" || tool.ResumeByIDCommand == "" || tool.ResumePickerCommand != "" {
		return ""
	}
	return fmt.Sprintf("revived %s with --continue: no conversation id captured, may resume the wrong conversation", sess.Name)
}

// reviveSession relaunches one dead session under its old id, keeping its
// name, group, and history. When the session's own conversation id was
// captured, it resumes that exact conversation via the tool's
// resume_by_id_command instead of the working directory's most recent one,
// which would be the wrong conversation whenever sessions share a cwd.
func (m *Model) reviveSession(sess store.Session) error {
	if m.tmux.Exists(sess.ID) {
		return fmt.Errorf("session %s is still running; revive only applies to dead sessions", sess.Name)
	}
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if !isDir(sess.Cwd) {
		return fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	bind := func() error {
		launchedAt := time.Now()
		if err := m.store.SetAgentLaunchedAt(sess.ID, launchedAt); err != nil {
			return err
		}
		m.bindReviveLocally(sess.ID, launchedAt)
		return nil
	}
	if err := sessioncmd.SnapshotRelaunch(m.store, sess, tool, sess.AgentSessionID); err != nil {
		return err
	}
	if err := m.relaunchSession(sess, tool, launch.ReviveCommand(tool, sess.AgentSessionID), status.Starting, bind); err != nil {
		return err
	}
	if sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
		sessioncmd.InjectPickerKeys(m.tmux, sess.ID, tool.InputPrefix, tool.ResumePickerKeys)
	}
	m.rebuildRows()
	return nil
}

// relaunchSession puts a dead session's row back on a running tmux window
// under its old id, keeping its name, group and history. Both revive and
// restart end here; they differ only in the command they hand it and in
// bindConversation, which records the conversation the new pane is on once
// the launch has actually taken. A launch that fails leaves the row exactly
// as it was, still pointing at the conversation it can be revived on.
func (m *Model) relaunchSession(sess store.Session, tool config.Tool, baseCommand, newStatus string, bindConversation func() error) error {
	command, env, err := m.buildLaunch(sess.Tool, tool, baseCommand, sess.ID)
	if err != nil {
		return err
	}
	paneWidth, paneHeight := m.paneTargetSize()
	if err := m.tmux.Create(sess.ID, sess.Cwd, command, env, paneWidth, paneHeight); err != nil {
		return err
	}
	m.markFreshPane(sess.ID)
	if bindConversation != nil {
		if err := bindConversation(); err != nil {
			_ = m.tmux.Kill(sess.ID)
			return err
		}
	}
	// The row now lives on this manager's server, wherever it ran before.
	// Stamped after the launch has taken, so a row that never came back is
	// not left pointing at a server that holds no pane for it, and a row
	// that cannot take the stamp is gone and its fresh pane goes with it.
	if err := m.store.SetTmuxSocket(sess.ID, m.tmux.SocketPath()); err != nil {
		_ = m.tmux.Kill(sess.ID)
		return err
	}
	if err := m.tmux.SetLabel(sess.ID, sessionLabel(sess.Group, sess.Name)); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, newStatus); err != nil {
		return err
	}
	// The session is alive again; any watcher backoff from its dead spell
	// no longer applies.
	if m.focus != nil {
		m.focus.retryNow()
	}
	// A leftover ack from the previous life must not swallow the relaunched
	// agent's first finished alert.
	return m.store.SetAcked(sess.ID, false)
}

// relaunchedMsg carries the result of starting an agent in a pane that was
// left on its shell.
type relaunchedMsg struct {
	sessID     string
	launchedAt time.Time
	err        error
}

// relaunchInPane builds the command that starts a session's tool again
// inside the shell its pane already holds, for a session whose window is
// alive because only the agent exited. The command is typed into that
// shell rather than launched over a fresh window, so nothing about the
// pane is lost and the agent comes back as the shell's child, the shape
// every other session has. It carries the session environment inline as
// well, since a pane opened by an older manager holds a shell that was
// never given it. The probe and the send run off the update path, where a
// pane that answers slowly would hold up the whole UI.
func (m *Model) relaunchInPane(sess store.Session) (tea.Cmd, error) {
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return nil, fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if tool.Shell {
		return nil, fmt.Errorf("%s is a shell; its pane is already open", sess.Name)
	}
	if !isDir(sess.Cwd) {
		return nil, fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	driver, stor, hookManager := m.tmux, m.store, m.hooks
	return func() tea.Msg {
		launchedAt, err := sessioncmd.RelaunchInPane(driver, stor, hookManager, sess, tool)
		if err != nil {
			return relaunchedMsg{sessID: sess.ID, err: err}
		}
		if sess.AgentSessionID == "" && tool.ResumePickerKeys != "" {
			sessioncmd.InjectPickerKeys(driver, sess.ID, tool.InputPrefix, tool.ResumePickerKeys)
		}
		return relaunchedMsg{sessID: sess.ID, launchedAt: launchedAt}
	}, nil
}

// restartSelected asks to relaunch the selected session with an empty
// context: the same row, directory and tool, running a brand new
// conversation instead of resuming the one it held.
func (m *Model) restartSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		m.errBar.text = "restart applies to a session; pick one under " + displayGroup(entry.group)
		return m, nil
	}
	label := fmt.Sprintf("restart %s with an empty context? its current conversation is left behind.", entry.sess.Name)
	if m.tmux.Exists(entry.sess.ID) {
		label = fmt.Sprintf("restart %s with an empty context? ends the running agent and leaves its conversation behind.", entry.sess.Name)
	}
	m.confirm = confirmTarget{
		action:   actionRestart,
		sessions: []store.Session{entry.sess},
		label:    label,
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// restartSession relaunches a session's tool from scratch. The conversation
// it was resuming is retired rather than resumed, so the agent comes back
// with the same name, directory and group but no context to carry.
func (m *Model) restartSession(sess store.Session) error {
	tool, ok := m.cfg.Tools[sess.Tool]
	if !ok {
		return fmt.Errorf("tool %s is no longer configured", sess.Tool)
	}
	if !isDir(sess.Cwd) {
		return fmt.Errorf("working directory no longer exists: %s", sess.Cwd)
	}
	if err := m.killSession(sess); err != nil {
		return err
	}
	baseCommand, agentSessionID := restartLaunch(tool)
	if err := sessioncmd.SnapshotRelaunch(m.store, sess, tool, agentSessionID); err != nil {
		return err
	}
	bind := func() error {
		launchedAt := time.Now()
		if err := m.store.RestartAgent(sess.ID, agentSessionID, launchedAt); err != nil {
			return err
		}
		m.bindRestartLocally(sess.ID, agentSessionID, launchedAt)
		return nil
	}
	// Starting, like a fresh spawn: the row reads as booting until the new
	// agent paints its pane.
	return m.relaunchSession(sess, tool, baseCommand, status.Starting, bind)
}

// restartLaunch builds what a restart runs: the tool's plain launch command,
// exactly as a brand new session gets it, plus a fresh conversation id for
// the tools that take one. Tools that mint their own id instead get nothing
// to carry, and the poller captures what they wrote.
func restartLaunch(tool config.Tool) (baseCommand, agentSessionID string) {
	if tool.SessionIDFlag == "" {
		return tool.Command, ""
	}
	agentSessionID = uuid.NewString()
	return tool.Command + " " + tool.SessionIDFlag + " " + agentSessionID, agentSessionID
}

// bindRestartLocally mirrors the store write in the loaded rows, so the list
// redraws on the new conversation before the next poll re-reads it.
func (m *Model) bindRestartLocally(id, agentSessionID string, launchedAt time.Time) {
	for i := range m.sessions {
		if m.sessions[i].ID != id {
			continue
		}
		if m.sessions[i].AgentSessionID != "" {
			m.sessions[i].RetiredAgentSessionID = m.sessions[i].AgentSessionID
		}
		m.sessions[i].AgentSessionID = agentSessionID
		m.sessions[i].AgentLaunchedAt = launchedAt
		m.sessions[i].Status = status.Starting
		m.sessions[i].Acked = false
	}
}

// bindReviveLocally mirrors the store write in the loaded rows, so the
// startup loader can paint before the next poll re-reads them.
func (m *Model) bindReviveLocally(id string, launchedAt time.Time) {
	for i := range m.sessions {
		if m.sessions[i].ID != id {
			continue
		}
		m.sessions[i].AgentLaunchedAt = launchedAt
		m.sessions[i].Status = status.Starting
		m.sessions[i].Acked = false
	}
	if sel, ok := m.selected(); ok && sel.ID == id {
		// A killed or archived row still holds its last pane; that would
		// count as painted and hide the loader.
		m.preview = ""
	}
}

// killSelected asks to end the selected session, or every live session
// under the selected group, freeing the RAM their agents hold while the
// rows stay put for v to revive.
func (m *Model) killSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		live, err := m.liveSessions(m.sessionsInGroup(entry.group))
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		if len(live) == 0 {
			m.errBar.text = "no live sessions to kill in " + entry.group
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionKill,
			sessions: live,
			label: fmt.Sprintf("kill group %s (%d live sessions)? frees their RAM, v revives them.",
				entry.group, len(live)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		live := false
		for _, sess := range sessions {
			if m.tmux.Exists(sess.ID) {
				live = true
				break
			}
		}
		if !live {
			m.errBar.text = entry.sess.Name + " is already dead"
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionKill,
			sessions: sessions,
			label: followConfirmLabel("kill", entry.sess.Name, len(sessions)-1,
				"frees its RAM, v revives it.",
				"frees their RAM, v revives them."),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// killAllLive asks to end every live session in the current view, the
// batch counterpart to V.
func (m *Model) killAllLive() (tea.Model, tea.Cmd) {
	live, err := m.liveSessions(m.listedSessions())
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	if len(live) == 0 {
		m.errBar.text = "no live sessions to kill"
		return m, nil
	}
	m.confirm = confirmTarget{
		action:   actionKill,
		sessions: live,
		label:    fmt.Sprintf("kill every live session (%d)? frees their RAM, v revives them.", len(live)),
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// liveSessions narrows a list to the sessions that still hold a tmux
// window. One pane listing answers for all of them, so a wide selection
// costs one tmux call rather than one per session.
func (m *Model) liveSessions(sessions []store.Session) ([]store.Session, error) {
	panes, err := m.tmux.Panes()
	if err != nil {
		return nil, err
	}
	var live []store.Session
	for _, sess := range sessions {
		if panes[sess.ID].PID > 0 {
			live = append(live, sess)
		}
	}
	return live, nil
}

// killSession ends one session's tmux window, freeing everything its agent
// held, while the store row keeps the name, group, history and conversation
// id that revive needs. The pane is captured first so the preview still
// shows the agent's last output once the window is gone.
func (m *Model) killSession(sess store.Session) error {
	if !m.tmux.Exists(sess.ID) {
		return nil
	}
	if pane, err := m.tmux.CapturePane(sess.ID); err == nil && pane != "" {
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	var killErr error
	// Runs under the poller's lock so no pass can capture a half-killed
	// pane, and drops the pane hash the revived session would be compared
	// against.
	m.poller.reflowSessions([]string{sess.ID}, func() {
		killErr = m.tmux.Kill(sess.ID)
	})
	if killErr != nil {
		return killErr
	}
	// The agent dies without running its session-end hook, so a leftover
	// status file would otherwise decide what the revived session reads as.
	if err := m.hooks.Remove(sess.ID); err != nil {
		return err
	}
	if err := m.store.UpdateStatus(sess.ID, status.Dead); err != nil {
		return err
	}
	for i := range m.sessions {
		if m.sessions[i].ID == sess.ID {
			m.sessions[i].Status = status.Dead
		}
	}
	return nil
}

func (m *Model) archiveSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionArchive,
			sessions: subtree,
			label:    fmt.Sprintf("archive group %s (%d sessions)? frees their RAM, t to find them.", entry.group, len(subtree)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionArchive,
			sessions: sessions,
			label: followConfirmLabel("archive", entry.sess.Name, len(sessions)-1,
				"frees its RAM, t to find it.",
				"frees their RAM, t to find them."),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

func (m *Model) restoreSelected() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		return m, nil
	}
	if entry.isGroup {
		subtree, err := m.store.SessionsInSubtree(entry.group)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			isGroup:  true,
			path:     entry.group,
			action:   actionRestore,
			sessions: subtree,
			label:    fmt.Sprintf("restore group %s (%d sessions)? brings them back.", entry.group, len(subtree)),
		}
	} else {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return m, nil
		}
		m.confirm = confirmTarget{
			action:   actionRestore,
			sessions: sessions,
			label: followConfirmLabel("restore", entry.sess.Name, len(sessions)-1,
				"brings it back.",
				"brings them back."),
		}
	}
	m.mode = modeConfirmDelete
	return m, nil
}

// snapshotLive stores the last pane of every still-live session. Archive
// calls it before any kill so a capture failure cannot drop a frame.
func (m *Model) snapshotLive(sessions []store.Session) error {
	for _, sess := range sessions {
		if !m.tmux.Exists(sess.ID) {
			continue
		}
		pane, err := m.tmux.CapturePane(sess.ID)
		if err != nil {
			return err
		}
		if pane == "" {
			continue
		}
		if err := m.setSnapshot(sess.ID, pane); err != nil {
			return err
		}
	}
	return nil
}

func (m *Model) applyConfirmedArchived(archived bool) error {
	if m.confirm.isGroup {
		return m.store.SetGroupArchived(m.confirm.path, archived)
	}
	for _, sess := range m.confirm.sessions {
		if err := m.store.SetArchived(sess.ID, archived); err != nil {
			return err
		}
		m.forgetLaunch(sess.ID)
	}
	return nil
}

// removeSessionLocally takes a deleted row off the loaded list right away,
// so it leaves the screen on this frame instead of waiting for the next
// poll to confirm what the store already knows.
func (m *Model) removeSessionLocally(id string) {
	for i := range m.sessions {
		if m.sessions[i].ID == id {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			break
		}
	}
	m.markSession(id, goneMark{deleted: true})
	m.rebuildRows()
}

// markArchivedLocally flags the confirmed archive in the loaded rows, so
// they leave the active view on this frame instead of first showing the
// dead state their kill just gave them. A group archive also flags the
// group itself and every group under it, which is what hides the whole
// subtree from the active view.
func (m *Model) markArchivedLocally(sessions []store.Session, groupPath string) {
	changed := false
	for i := range m.sessions {
		if m.sessions[i].Archived {
			continue
		}
		for _, sess := range sessions {
			if m.sessions[i].ID != sess.ID {
				continue
			}
			m.sessions[i].Archived = true
			m.markSession(sess.ID, goneMark{archived: true})
			changed = true
			break
		}
	}
	if groupPath != "" {
		for _, path := range append([]string{groupPath}, m.subgroupPaths(groupPath)...) {
			if m.archivedGroups == nil {
				m.archivedGroups = map[string]bool{}
			}
			m.archivedGroups[path] = true
			m.markGroup(path, goneMark{archived: true})
		}
		changed = true
	}
	if changed {
		m.rebuildRows()
	}
}

func (m *Model) subgroupPaths(path string) []string {
	var out []string
	prefix := path + "/"
	for _, group := range m.groups {
		if strings.HasPrefix(group, prefix) {
			out = append(out, group)
		}
	}
	return out
}

// markRestoredLocally mirrors a completed restore in the loaded rows and
// group flags, so what came back changes views on this frame rather than
// waiting for the next poll. A group restore unarchives the subtree; a
// single restore also clears its group's ancestors, which is what the
// store write just did to keep a restored session under a live home.
func (m *Model) markRestoredLocally(restored []store.Session, groupPath string) {
	byID := make(map[string]bool, len(restored))
	for _, sess := range restored {
		byID[sess.ID] = true
	}
	for i := range m.sessions {
		if !m.sessions[i].Archived {
			continue
		}
		if byID[m.sessions[i].ID] || (groupPath != "" && inGroupSubtree(m.sessions[i].Group, groupPath)) {
			m.sessions[i].Archived = false
			m.markSession(m.sessions[i].ID, goneMark{archived: false})
		}
	}
	if groupPath != "" {
		for _, path := range append([]string{groupPath}, m.subgroupPaths(groupPath)...) {
			delete(m.archivedGroups, path)
			m.markGroup(path, goneMark{archived: false})
		}
	} else {
		for _, sess := range restored {
			for path := sess.Group; path != ""; path = parentGroup(path) {
				delete(m.archivedGroups, path)
				m.markGroup(path, goneMark{archived: false})
			}
		}
	}
	m.rebuildRows()
}

// markGroup records the archive state this run just gave a group path,
// so stale polls predating the change are reconciled on arrival instead
// of undoing it for a frame.
func (m *Model) markGroup(path string, mark goneMark) {
	if m.goneGroups == nil {
		m.goneGroups = map[string]goneMark{}
	}
	mark.at = time.Now()
	m.goneGroups[path] = mark
}

// pruneGroupsLocally drops the removed group paths from the loaded tree,
// so a deleted group's header goes with its sessions instead of hanging
// around empty until the next poll. Each path is recorded for the stale
// listing filter, which is what keeps an in-flight poll from restoring it.
func (m *Model) pruneGroupsLocally(removed []string) {
	gone := make(map[string]bool, len(removed))
	for _, path := range removed {
		gone[path] = true
		m.markGroup(path, goneMark{deleted: true})
	}
	groups := make([]string, 0, len(m.groups))
	for _, group := range m.groups {
		if !gone[group] {
			groups = append(groups, group)
		}
	}
	m.groups = groups
	for _, path := range removed {
		delete(m.groupPaths, path)
		delete(m.groupWorktrees, path)
		delete(m.archivedGroups, path)
	}
	m.rebuildRows()
}

func (m *Model) sessionAndChildren(sess store.Session) ([]store.Session, error) {
	kids, err := m.store.Children(sess.ID)
	if err != nil {
		return nil, err
	}
	out := make([]store.Session, 0, 1+len(kids))
	out = append(out, sess)
	return append(out, kids...), nil
}

// childrenFirst orders a follow-set so terminals go before the agent they
// hang under: a cleanup that fails partway leaves no row pointing at a
// parent that is already gone.
func childrenFirst(sessions []store.Session) []store.Session {
	ordered := make([]store.Session, 0, len(sessions))
	for _, sess := range sessions {
		if sess.ParentID != "" {
			ordered = append(ordered, sess)
		}
	}
	for _, sess := range sessions {
		if sess.ParentID == "" {
			ordered = append(ordered, sess)
		}
	}
	return ordered
}

func followConfirmLabel(verb, name string, extra int, one, many string) string {
	if extra <= 0 {
		return fmt.Sprintf("%s %s? %s", verb, name, one)
	}
	unit := "terminal"
	if extra != 1 {
		unit = "terminals"
	}
	return fmt.Sprintf("%s %s and %d %s? %s", verb, name, extra, unit, many)
}

func groupPath(confirm confirmTarget) string {
	if confirm.isGroup {
		return confirm.path
	}
	return ""
}

func (m *Model) prepareDelete() {
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.errBar.text = "root is the top level; delete the sessions under it instead"
		return
	}
	if !entry.isGroup {
		sessions, err := m.sessionAndChildren(entry.sess)
		if err != nil {
			m.errBar.text = err.Error()
			return
		}
		m.confirm = confirmTarget{
			label: followConfirmLabel("delete", entry.sess.Name, len(sessions)-1,
				"kills its tmux session.",
				"kills their tmux sessions."),
			sessions: sessions,
		}
		m.mode = modeConfirmDelete
		return
	}
	subtree, err := m.store.SessionsInSubtree(entry.group)
	if err != nil {
		m.errBar.text = err.Error()
		return
	}
	if m.showArchived {
		m.confirm = archivedGroupDelete(entry.group, subtree)
	} else {
		m.confirm = m.wholeGroupDelete(entry.group, subtree)
	}
	m.mode = modeConfirmDelete
}

// wholeGroupDelete targets the group as the active view shows it: the
// group ceases to exist, so its subtree goes with it, archived sessions
// included, leaving nothing stranded under a group that is gone.
func (m *Model) wholeGroupDelete(path string, subtree []store.Session) confirmTarget {
	subgroups := 0
	for _, g := range m.groups {
		if strings.HasPrefix(g, path+"/") {
			subgroups++
		}
	}
	return confirmTarget{
		isGroup:  true,
		path:     path,
		sessions: subtree,
		label: fmt.Sprintf("delete group %s (%d subgroups, %d sessions incl. archived)? kills their tmux sessions.",
			path, subgroups, len(subtree)),
	}
}

// archivedGroupDelete targets only what the archived view shows: the
// archived sessions under the group. The live sessions and the group
// itself belong to the active view and survive; the group row goes only
// once nothing is left beneath it.
func archivedGroupDelete(path string, subtree []store.Session) confirmTarget {
	var archived []store.Session
	for _, sess := range subtree {
		if sess.Archived {
			archived = append(archived, sess)
		}
	}
	return confirmTarget{
		isGroup:      true,
		archivedOnly: true,
		path:         path,
		sessions:     archived,
		label: fmt.Sprintf("delete %s from the archive (%d archived sessions)? kills their tmux sessions, live ones stay.",
			path, len(archived)),
	}
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A relaunch the manager refused opened the hint dialog; every other
	// answer falls back to the list.
	defer func() {
		if m.mode != modeLaunchHint {
			m.mode = modeList
		}
	}()
	switch msg.String() {
	case "y", "enter":
		switch m.confirm.action {
		case actionArchive:
			if err := m.snapshotLive(m.confirm.sessions); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
			}
			if err := m.applyConfirmedArchived(true); err != nil {
				m.errBar.text = err.Error()
				return m, nil
			}
			m.markArchivedLocally(m.confirm.sessions, groupPath(m.confirm))
			m.errBar.text = ""
		case actionRestore:
			// Each session leaves the archive as it comes back, so a later
			// failure cannot strand a running one in the archived view.
			for _, sess := range m.confirm.sessions {
				if !m.tmux.Exists(sess.ID) {
					if err := m.reviveSession(sess); err != nil {
						m.reportLaunchError(err)
						return m, nil
					}
				}
				if m.confirm.isGroup {
					continue
				}
				if err := m.store.SetArchived(sess.ID, false); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				m.markSession(sess.ID, goneMark{archived: false})
			}
			if m.confirm.isGroup {
				if err := m.applyConfirmedArchived(false); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
			}
			m.markRestoredLocally(m.confirm.sessions, groupPath(m.confirm))
			m.errBar.text = ""
		case actionKill:
			for _, sess := range m.confirm.sessions {
				if err := m.killSession(sess); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRestart:
			for _, sess := range m.confirm.sessions {
				if err := m.restartSession(sess); err != nil {
					m.reportLaunchError(err)
					return m, nil
				}
			}
			m.errBar.text = ""
			m.rebuildRows()
		case actionRevive:
			for _, sess := range m.confirm.sessions {
				if m.tmux.Exists(sess.ID) {
					continue
				}
				if err := m.reviveSession(sess); err != nil {
					m.reportLaunchError(err)
					return m, nil
				}
			}
			m.errBar.text = ""
		case actionDelete:
			for _, sess := range childrenFirst(m.confirm.sessions) {
				if err := m.tmux.Kill(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				if err := m.hooks.Remove(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveName(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewRepo(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewBase(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				if err := m.hooks.RemoveReviewScope(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				delete(m.pickedRepos, sess.ID)
				delete(m.awaitedRenames, sess.ID)
				m.forgetLaunch(sess.ID)
				if err := m.store.Delete(sess.ID); err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				m.removeSessionLocally(sess.ID)
				if sess.WorktreeRepo != "" && m.gitDrv != nil {
					used, err := m.sessionUsesDir(sess.Cwd)
					if err != nil {
						m.errBar.text = "worktree cleanup: " + err.Error()
					} else if used {
						m.errBar.text = "worktree kept (used by another session): " + sess.Cwd
					} else if removed, err := m.gitDrv.RemoveWorktreeIfClean(sess.WorktreeRepo, sess.Cwd, sess.WorktreeBranch); err != nil {
						m.errBar.text = "worktree cleanup: " + err.Error()
					} else if !removed {
						m.errBar.text = "worktree kept (has work): " + sess.Cwd
					}
				}
			}
			if m.confirm.isGroup {
				removed, err := m.deleteConfirmedGroups()
				if err != nil {
					m.errBar.text = err.Error()
					return m, nil
				}
				for _, path := range removed {
					delete(m.collapsed, path)
				}
				m.persistCollapsed()
				m.pruneGroupsLocally(removed)
			}
		default:
			m.errBar.text = fmt.Sprintf("unknown confirm action %q", m.confirm.action)
			return m, nil
		}
		m.confirm = confirmTarget{}
		m.requestRefresh()
		return m, nil
	}
	m.confirm = confirmTarget{}
	return m, nil
}

func (m *Model) sessionUsesDir(dir string) (bool, error) {
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		return false, err
	}
	for _, sess := range sessions {
		if sess.Cwd == dir {
			return true, nil
		}
	}
	return false, nil
}

// deleteConfirmedGroups removes the group rows the confirmed delete
// covers, reporting the paths that went so their fold state can go too.
func (m *Model) deleteConfirmedGroups() ([]string, error) {
	if m.confirm.archivedOnly {
		return m.store.PruneArchivedGroups(m.confirm.path)
	}
	return m.store.DeleteGroup(m.confirm.path)
}
