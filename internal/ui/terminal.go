package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// terminalKeyWindow is how long after handling a T another one reads as
// autorepeat rather than a second request: longer than the pause before a
// held key starts repeating and than the interval it repeats at. It runs
// from the end of the spawn, so the quiet gap between two deliberate
// shells is this window plus the spawn.
const terminalKeyWindow = 250 * time.Millisecond

// shellTool finds the config block T spawns: the first one declaring
// shell = true, by name so the pick is the same on every launch. Nothing
// keys off the block being called "terminal", so a user who already has a
// [tools.terminal] block of their own keeps it as the agent CLI they wrote.
func (m *Model) shellTool() (string, config.Tool, bool) {
	return m.cfg.ShellTool()
}

// isShell reports whether a session's tool opens a shell rather than an
// agent. A tool no longer in the config answers false: an unknown block is
// not something we can claim runs a shell.
func (m *Model) isShell(toolName string) bool {
	return m.cfg.Tools[toolName].Shell
}

// terminalKey spawns a shell unless T arrived inside the burst a held key
// sends. The window is measured from the end of the previous T's work, not
// from its arrival: the spawn runs on the update path, so the keystroke a
// burst queued behind it is only read once it finishes, and an interval
// measured from arrival would be the spawn's own duration rather than the
// gap the keyboard produced. Each keystroke pushes the window out, so
// holding T spawns one shell however long it is held. The other keys that
// create something open a form first, which absorbs a burst on its own.
func (m *Model) terminalKey() (tea.Model, tea.Cmd) {
	if time.Since(m.terminalKeyAt) < terminalKeyWindow {
		m.terminalKeyAt = time.Now()
		return m, nil
	}
	model, cmd := m.openTerminal()
	m.terminalKeyAt = time.Now()
	return model, cmd
}

// openTerminal spawns a shell tab in the group under the cursor. The block
// carries no command, and tmux.Create leaves such a pane on the user's
// shell, so no prompt, rename directive or MCP registration applies to a
// session there is no agent to send them to.
func (m *Model) openTerminal() (tea.Model, tea.Cmd) {
	toolName, tool, ok := m.shellTool()
	if !ok {
		m.errBar.text = `no shell configured: add a tool block with shell = true to config.toml`
		return m, nil
	}
	dir, ok := m.rowDir()
	if !ok {
		m.errBar.text = "no directory to open a terminal in: " + dir
		return m, nil
	}
	// Named and linked for the session it was opened on, the way a run script
	// already is: a shell called after four random hex digits says nothing
	// about which of a group's worktrees it landed in.
	name := toolName + "-" + newID()[:4]
	parent, fromSession := m.shellOrigin()
	if fromSession {
		name = m.unusedName(toolName + "-" + parent.Name)
	}
	sess := store.Session{
		ID:       newID(),
		Name:     name,
		Tool:     toolName,
		Cwd:      dir,
		Group:    m.contextGroup(),
		Status:   status.Starting,
		ParentID: parent.ID,
	}
	if err := m.launchNewSession(sess, tool, tool.Command, launchOptions{}); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// Starting sits outside the attention set, so the row the key just made
	// would be filtered off screen.
	m.statusFilter = statusFilterAll
	m.errBar.text = ""
	m.focusSession(sess.ID)
	return m, m.refreshCmd()
}

// shellOrigin is the session a shell opened here belongs to: the agent under
// the cursor, or that agent again when the cursor is already on one of its
// shells, so a second terminal joins the first rather than hanging off it.
// A group row has no session to hang off and answers false.
//
// The link is resolved against every session rather than against the index
// the rail painted from, which a search narrows: a query matching shells but
// not agents would otherwise store an empty parent on a terminal that has
// one, and an empty parent is not a row that redraws — it is what the
// directory fallback later reads, and it can land on a different agent.
func (m *Model) shellOrigin() (store.Session, bool) {
	entry, ok := m.selectedRow()
	if !ok || entry.isGroup {
		return store.Session{}, false
	}
	if m.isShell(entry.sess.Tool) {
		return m.sessionByID(m.shellParentIndex(m.sessions)[entry.sess.ID])
	}
	return entry.sess, true
}

// unusedName keeps a name generated from a session's own unique among the
// sessions there are: two shells opened on the same agent would otherwise
// land on one name, and the name is how a row is told from its sibling. It
// counts rather than reaching for an id, so the second shell on a session
// reads as the second one.
func (m *Model) unusedName(name string) string {
	taken := make(map[string]bool, len(m.sessions))
	for _, sess := range m.sessions {
		taken[sess.Name] = true
	}
	if !taken[name] {
		return name
	}
	// One candidate per session already named plus this one is always more
	// than the names in the way, so the search cannot run past the end.
	for n := 2; n <= len(taken)+2; n++ {
		candidate := fmt.Sprintf("%s-%d", name, n)
		if !taken[candidate] {
			return candidate
		}
	}
	return name
}

// shellParentIndex maps each shell in sessions to the session it hangs off.
// The link a shell recorded when it was opened wins; a shell whose link is
// missing or points somewhere this view does not hold falls back to the
// oldest agent launched in the same directory and group, which for a
// worktree is the one agent living there. Both are resolved against the set
// the tree is built from, so a shell never points at a row the view is
// holding back, and never out of the group it carries.
func (m *Model) shellParentIndex(sessions []store.Session) map[string]string {
	// Keyed by group as well as directory: a shell never nests outside the
	// group it carries, so an older agent in another group pointed at the
	// same checkout is not a candidate at all. Holding one agent per
	// directory would let that agent take the slot and lose the shell a
	// parent it did have.
	type dirKey struct{ group, cwd string }
	agents := map[string]store.Session{}
	oldestInDir := map[dirKey]store.Session{}
	for _, sess := range sessions {
		if m.isShell(sess.Tool) {
			continue
		}
		agents[sess.ID] = sess
		if sess.Cwd == "" {
			continue
		}
		key := dirKey{group: sess.Group, cwd: sess.Cwd}
		if held, seen := oldestInDir[key]; !seen || sess.CreatedAt.Before(held.CreatedAt) {
			oldestInDir[key] = sess
		}
	}
	parents := map[string]string{}
	for _, sess := range sessions {
		if !m.isShell(sess.Tool) {
			continue
		}
		parent, found := agents[sess.ParentID]
		if !found && sess.Cwd != "" {
			parent, found = oldestInDir[dirKey{group: sess.Group, cwd: sess.Cwd}]
		}
		if found && parent.Group == sess.Group {
			parents[sess.ID] = parent.ID
		}
	}
	return parents
}

// shellsOpenedFor is the shells a session owns, read from the store rather
// than from the rail: a delete that skipped the archived ones, or the ones a
// search is holding back, would strand exactly the shells nobody can see to
// clean up. Only the recorded link counts here — the shell was opened for
// this session whatever it has since been cd'd to, which is the one question
// the directory cannot answer.
func (m *Model) shellsOpenedFor(id string) ([]store.Session, error) {
	sessions, err := m.store.ListSessions(true)
	if err != nil {
		return nil, err
	}
	var shells []store.Session
	for _, sess := range sessions {
		if sess.ParentID == id && m.isShell(sess.Tool) {
			shells = append(shells, sess)
		}
	}
	return shells, nil
}

// shellOriginLabel is what a shell row says about where it belongs, in the
// column an agent gives to its tool. Nested, the session it hangs off is the
// row above and the column stays empty. Pinned, the block stands away from
// the tree, so the row names that session itself; the group it carries is
// shared by every worktree under it and tells them apart from nothing. With
// no session either way it falls back to the directory it was launched in,
// which is still narrower than the group.
func (m *Model) shellOriginLabel(sess store.Session) string {
	if parent, linked := m.sessionByID(m.shellParents[sess.ID]); linked {
		if !m.pinnedShell(sess) {
			return ""
		}
		return parent.Name
	}
	if sess.Cwd == "" {
		return displayGroup(sess.Group)
	}
	return filepath.Base(sess.Cwd)
}

// rowDir is the directory the cursor points at: a live session's pane
// directory, which follows wherever its shell or agent moved, falling back
// to the directory it was created in; for a group, its default path. Both
// paths are checked, so a directory removed under a running session cannot
// hand back somewhere that is no longer there.
func (m *Model) rowDir() (string, bool) {
	entry, ok := m.selectedRow()
	if !ok {
		return "", false
	}
	if entry.isGroup {
		return resolveExistingDir(m.groupPaths[entry.group], m.groupDefaultDir(entry.group))
	}
	if path, err := m.tmux.PaneCurrentPath(entry.sess.ID); err == nil && isDir(path) {
		return path, true
	}
	return entry.sess.Cwd, isDir(entry.sess.Cwd)
}
