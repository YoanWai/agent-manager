package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

type spawnKind int

const (
	spawnForm spawnKind = iota
	spawnQuick
	spawnShell
)

// spawnRequest captures everything a spawn needs at dispatch time, so the
// worker never reads the Model: the tool, the captured launch inputs, the
// pane size the manager draws, and the images the prompt names, moved out
// of the composer that held them.
type spawnRequest struct {
	id           string
	kind         spawnKind
	toolName     string
	name         string
	dir          string
	group        string
	parentID     string
	prompt       string
	autoNamed    bool
	worktree     bool
	pickWorktree bool
	pane         sessioncmd.PaneSize
	composerGen  int
	draftName    string
	draftDir     string
	draft        string
	rawDir       string
	dirFallbacks []string
	wantWorktree bool
	dirReader    directoryPreflight
	terminalDir  terminalDirectoryRequest
	terminalRead terminalDirectoryReader
	plan         *launch.Plan
	images       []imageAttachment
	choice       config.Choice
	base         string
}

func (spawnRequest) effectRequest() {}

// spawnEffectResult is the durable outcome of a spawn: the persisted
// session row, which exists only when the pane was created too, and a label
// failure that stays a warning after committed placement.
type spawnEffectResult struct {
	plan        *launch.Plan
	session     store.Session
	label       error
	dir         string
	worktree    bool
	repoKnown   bool
	repoCapable bool
}

func (spawnEffectResult) effectResult() {}

// groupRequest captures a group creation; the accepted effect validates the
// directory before the store write.
// gen identifies the dispatching group form so a completion cannot replace
// a form the user has since reopened.
type groupRequest struct {
	draftName string
	draftDir  string
	path      string
	dir       string
	worktree  string
	base      string
	gen       int
	rawDir    string
	fallbacks []string
	dirReader directoryPreflight
}

func (groupRequest) effectRequest() {}

type groupEffectResult struct {
	path     string
	dir      string
	worktree string
}

func (groupEffectResult) effectResult() {}

// dispatchSpawn takes the prompt's images out of the composer and queues
// the spawn on the shared effect lane; completion routes the images.
func (m *Model) dispatchSpawn(request spawnRequest) {
	for _, job := range append([]*effectJob{m.effects.main.active}, m.effects.main.pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.request.(spawnRequest); ok && pending.kind == request.kind && pending.composerGen == request.composerGen {
			m.reportErr("this spawn is already in progress")
			return
		}
	}
	request.id = newID()
	switch request.kind {
	case spawnForm:
		request.draft = m.form.prompt.input.Value()
		request.draftName = m.form.Name()
		request.draftDir = m.form.Dir()
		m.form.prompt.attachments = nil
	case spawnQuick:
		request.draft = m.quick.input.Value()
		m.quick.attachments = nil
	}
	m.enqueueEffect(request, 0, false)
}

func (s effectServices) runSpawn(request spawnRequest) (effectResult, error) {
	result := spawnEffectResult{}
	if s.lifecycle == nil {
		return result, errors.New("spawn requires the lifecycle service")
	}
	tool := s.cfg.Tools[request.toolName].WithChoice(request.choice)
	dir := request.dir
	if dir == "" {
		if request.kind == spawnShell {
			reader := request.terminalRead
			if reader == nil {
				reader = systemTerminalDirectoryReader{tmux: s.driver, dirs: systemDirectoryPreflight{git: s.gitDrv}}
			}
			var ok bool
			dir, ok = reader.resolve(request.terminalDir)
			if !ok {
				return result, errors.New("no directory to open a terminal in")
			}
		} else {
			reader := request.dirReader
			if reader == nil {
				reader = systemDirectoryPreflight{git: s.gitDrv}
			}
			var ok bool
			dir, ok = reader.resolve(request.rawDir, request.dirFallbacks)
			if !ok {
				prefix := "working directory does not exist: "
				if request.kind == spawnQuick {
					prefix = "group has no valid default path: "
				}
				return result, errors.New(prefix + dir)
			}
			if request.wantWorktree {
				result.repoKnown = true
				result.repoCapable = reader.repoCapable(dir)
				request.worktree = result.repoCapable
			}
		}
	}
	result.dir = dir
	result.worktree = request.worktree
	plan := launch.Plan{}
	if request.kind == spawnShell {
		plan.Command = tool.Command
	} else if request.plan != nil {
		plan = *request.plan
	} else {
		proactive, err := s.store.ProactiveCoordination()
		if err != nil {
			return result, err
		}
		plan = launch.Assemble(request.toolName, tool, request.prompt, request.autoNamed, proactive)
	}
	result.plan = &plan
	id := request.id
	if id == "" {
		id = newID()
	}
	var worktree *sessioncmd.WorktreeRequest
	if request.worktree {
		worktree = &sessioncmd.WorktreeRequest{Base: request.base}
	}
	launched, err := s.lifecycle.Spawn(sessioncmd.SpawnRequest{
		Session: store.Session{
			ID:       id,
			Name:     request.name,
			Tool:     request.toolName,
			Cwd:      dir,
			Group:    request.group,
			ParentID: request.parentID,
			Choice:   request.choice,
		},
		Tool:     tool,
		Plan:     plan,
		Pane:     request.pane,
		Worktree: worktree,
	})
	if err != nil {
		return result, err
	}
	result.session = launched.Session
	result.label = launched.LabelError
	return result, nil
}

func (s effectServices) runGroup(request groupRequest) (effectResult, error) {
	dir := request.dir
	if dir == "" {
		reader := request.dirReader
		if reader == nil {
			reader = systemDirectoryPreflight{git: s.gitDrv}
		}
		var ok bool
		dir, ok = reader.resolve(request.rawDir, request.fallbacks)
		if !ok {
			return groupEffectResult{}, errors.New("default path does not exist: " + dir)
		}
	}
	if err := s.store.AddGroup(request.path, dir, request.worktree, request.base); err != nil {
		return groupEffectResult{}, err
	}
	return groupEffectResult{path: request.path, dir: dir, worktree: request.worktree}, nil
}

func (m *Model) applySpawnEffect(request spawnRequest, result spawnEffectResult, err error) tea.Cmd {
	if result.repoKnown {
		if m.ledger.worktreeRepos == nil {
			m.ledger.worktreeRepos = make(map[string]repoAnswer)
		}
		m.ledger.worktreeRepos[result.dir] = repoAnswer{capable: result.repoCapable, at: time.Now()}
	}
	if err == nil {
		sess := result.session
		m.markFreshPane(sess.ID)
		if m.ledger.launched == nil {
			m.ledger.launched = map[string]time.Time{}
		}
		m.ledger.launched[sess.ID] = time.Now()
		m.workspace.sessions = append(m.workspace.sessions, sess)
		// The directive went out with the launch, so the row waits for the
		// name the agent picks instead of showing the one generated for it.
		if request.autoNamed {
			if m.ledger.awaitedRenames == nil {
				m.ledger.awaitedRenames = map[string]awaitedRename{}
			}
			m.ledger.awaitedRenames[sess.ID] = awaitedRename{generated: sess.Name, prompt: request.prompt}
		}
		if request.kind != spawnShell {
			m.rememberSpawnPick(request.toolName, request.pickWorktree)
			spawn.RememberModel(m.spawnHost(), request.toolName, spawn.Pick(request.choice))
		}
		// New sessions start as starting, which attention excludes; clear so
		// the row the spawn just created is on screen.
		m.rail.ClearStatusFilter()
		switch request.kind {
		case spawnForm:
			// A form opened later is a newer dialog the completion must not
			// close; only the one that dispatched this spawn is closed.
			if m.mode == modeForm && m.form.prompt.gen == request.composerGen && m.form.prompt.input.Value() == request.draft && m.form.Name() == request.draftName && m.form.Dir() == request.draftDir && len(m.form.prompt.attachments) == 0 {
				m.mode = modeList
			}
		case spawnQuick:
			if m.quick.active && m.quick.gen == request.composerGen && m.quick.input.Value() == request.draft && len(m.quick.attachments) == 0 {
				m.quick.clearAfterSend()
			}
		}
		m.rebuildRows()
		if request.kind == spawnShell {
			m.ledger.terminalKeyAt = time.Now()
			m.focusSession(sess.ID)
		}
		if result.label != nil {
			m.reportErr(result.label.Error())
		}
		return m.refreshCmd()
	}
	request.plan = result.plan
	request.dir = result.dir
	request.worktree = result.worktree
	if request.kind == spawnShell {
		m.ledger.terminalKeyAt = time.Now()
		m.reportErr(err.Error())
		return nil
	}
	// Nothing was committed. A spawn the hint dialog refuses takes the
	// dialog's place, the same way the synchronous submit did; a dialog the
	// user opened since the dispatch is newer and keeps the screen.
	takeOver := false
	switch {
	case request.kind == spawnForm && m.mode == modeForm && m.form.prompt.gen == request.composerGen && m.form.prompt.input.Value() == request.draft && len(m.form.prompt.attachments) == 0:
		takeOver = true
	case request.kind == spawnQuick && m.quick.active && m.quick.gen == request.composerGen && m.quick.input.Value() == request.draft && len(m.quick.attachments) == 0:
		takeOver = true
	case m.mode == modeList:
		takeOver = true
	}
	if takeOver {
		m.reportLaunchError(err)
		if m.mode == modeLaunchHint {
			// The dialog's images are the request's: the composer was emptied
			// at dispatch, so openLaunchHint found none of its own.
			m.launchHint.fix.images = append(m.launchHint.fix.images, request.images...)
			m.launchHint.fix.effectRetry = request
			if request.kind == spawnQuick {
				m.quick.active = false
			}
		} else {
			// A plain error left the composer on screen; its images go back.
			m.returnSpawnImages(request)
		}
		return nil
	}
	m.returnSpawnImages(request)
	m.reportErr(err.Error())
	return nil
}

// returnSpawnImages gives the prompt's images back to the composer that is
// still on screen, so the user can fix and resubmit; when no such composer
// remains, the files are dropped.
func (m *Model) returnSpawnImages(request spawnRequest) {
	switch request.kind {
	case spawnForm:
		if m.mode == modeForm && m.form.prompt.gen == request.composerGen {
			m.form.prompt.attachments = append(request.images, m.form.prompt.attachments...)
			return
		}
	case spawnQuick:
		if m.quick.active && m.quick.gen == request.composerGen {
			m.quick.attachments = append(request.images, m.quick.attachments...)
			return
		}
	}
	dropImages(request.images)
}

func (m *Model) applyGroupEffect(request groupRequest, result groupEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	// The durable inventory reconciles no matter what is on screen: the row
	// and its path/worktree are in the workspace, so the next render shows
	// them wherever the user is.
	m.materializeGroupsLocal([]string{result.path})
	if m.workspace.groupPaths == nil {
		m.workspace.groupPaths = map[string]string{}
	}
	m.workspace.groupPaths[result.path] = result.dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, result.path, result.worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, result.path, request.base)
	m.rebuildRows()
	// Presentation (closing the form, revealing and selecting the new group)
	// lands only when the dispatching group form is still the dialog on
	// screen; a newer one keeps the foreground. The reveal's collapse
	// snapshot queues after the row exists, the order the synchronous submit
	// kept.
	if m.mode == modeGroupForm && m.groupForm.Gen() == request.gen && m.groupForm.Name() == request.draftName && m.groupForm.Dir() == request.draftDir {
		m.clearErr()
		m.mode = modeList
		m.applyRailStateDecision(m.rail.RevealGroup(result.path))
	}
	return m.refreshCmd()
}

// choiceSaveRequest persists a CLI's kept choice or recent models. The
// settings cache already holds the value, so the next form reads it
// without waiting for the write.
type choiceSaveRequest struct {
	key   string
	value string
}

func (choiceSaveRequest) effectRequest() {}

type choiceSaveResult struct{}

func (choiceSaveResult) effectResult() {}

func (s effectServices) runChoiceSave(request choiceSaveRequest) (effectResult, error) {
	if err := s.store.SetSetting(request.key, request.value); err != nil {
		return choiceSaveResult{}, fmt.Errorf("saving %s: %w", request.key, err)
	}
	return choiceSaveResult{}, nil
}

// directoryPreflight is the read-only filesystem/repository seam captured by
// spawn and group effects. Its calls run inside the accepted FIFO worker.
type directoryPreflight interface {
	resolve(raw string, fallbacks []string) (string, bool)
	repoCapable(dir string) bool
}

type systemDirectoryPreflight struct{ git *git.Driver }

func (p systemDirectoryPreflight) resolve(raw string, fallbacks []string) (string, bool) {
	if raw != "" {
		return resolveExistingDir(raw, "")
	}
	var resolved string
	for _, fallback := range fallbacks {
		candidate, ok := resolveExistingDir("", fallback)
		resolved = candidate
		if ok {
			return candidate, true
		}
	}
	return resolved, false
}

func (p systemDirectoryPreflight) repoCapable(dir string) bool {
	if p.git == nil || dir == "" {
		return false
	}
	_, err := p.git.RepoRoot(dir)
	return err == nil
}

func initialWorkingDir() string {
	dir, _ := os.Getwd()
	return dir
}

func initialHomeDir() string {
	dir, _ := os.UserHomeDir()
	return dir
}

// resolveExistingDir turns raw field input into a usable directory:
// expand ~, fall back when empty, absolutize, and require it to exist.
// The resolved value returns either way so error messages can show it.
func resolveExistingDir(raw, fallback string) (string, bool) {
	dir := expandHome(strings.TrimSpace(raw))
	if dir == "" {
		dir = fallback
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir, isDir(dir)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

type worktreeProbeTarget uint8

const (
	worktreeProbeForm worktreeProbeTarget = iota
	worktreeProbeQuick
)

type worktreeProbeRequest struct {
	target     worktreeProbeTarget
	generation int
	sequence   uint64
	dir        string
	toggle     bool
	from       bool
}

type worktreeProbeMsg struct {
	request worktreeProbeRequest
	capable bool
}

func worktreeProbeCmd(request worktreeProbeRequest, reader directoryPreflight) tea.Cmd {
	return func() tea.Msg {
		return worktreeProbeMsg{request: request, capable: reader.repoCapable(request.dir)}
	}
}
