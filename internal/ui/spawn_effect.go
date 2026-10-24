package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
	"slices"
)

// The spawn kinds under the names the root's call sites spell them.
const (
	spawnForm  = uilaunch.Form
	spawnQuick = uilaunch.Quick
	spawnShell = uilaunch.Shell
)

// spawnRequest is the root's queue shape of a spawn: what the form, the quick
// bar or the terminal key captured. The run lives in ui/spawn/launch.
type spawnRequest struct {
	id           string
	kind         uilaunch.Kind
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
	images       []composer.Attachment
	choice       config.Choice
	base         string
}

func (spawnRequest) EffectRequest() {}

func (spawnRequest) effectName() string { return "spawn" }

func (value spawnRequest) copied() effectRequest {
	value.dirFallbacks = slices.Clone(value.dirFallbacks)
	value.terminalDir.fallbacks = slices.Clone(value.terminalDir.fallbacks)
	if value.plan != nil {
		plan := *value.plan
		plan.PendingInputs = slices.Clone(plan.PendingInputs)
		value.plan = &plan
	}
	value.images = slices.Clone(value.images)
	return value
}

func (request spawnRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runSpawn(request)
}

func (request spawnRequest) packageRequest() uilaunch.Request {
	id := request.id
	if id == "" {
		id = newID()
	}
	return uilaunch.Request{
		ID:           id,
		Kind:         request.kind,
		ToolName:     request.toolName,
		Name:         request.name,
		Dir:          request.dir,
		Group:        request.group,
		ParentID:     request.parentID,
		Prompt:       request.prompt,
		AutoNamed:    request.autoNamed,
		Worktree:     request.worktree,
		PickWorktree: request.pickWorktree,
		Pane:         request.pane,
		ComposerGen:  request.composerGen,
		DraftName:    request.draftName,
		DraftDir:     request.draftDir,
		Draft:        request.draft,
		RawDir:       request.rawDir,
		DirFallbacks: request.dirFallbacks,
		WantWorktree: request.wantWorktree,
		DirReader:    crossPreflight(request.dirReader),
		TerminalDir:  uilaunch.TerminalDirectoryRequest{PaneID: request.terminalDir.paneID, RawDir: request.terminalDir.rawDir, Fallbacks: request.terminalDir.fallbacks},
		TerminalRead: crossTerminalReader(request.terminalRead),
		Plan:         request.plan,
		Images:       request.images,
		Choice:       request.choice,
		Base:         request.base,
	}
}

type spawnEffectResult struct{ uilaunch.Result }

func (spawnEffectResult) EffectResult() {}

func (result spawnEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applySpawnEffect(job.Request.(spawnRequest), result, err)
}

// groupRequest captures a group creation; the accepted effect validates the
// directory before the store write. Gen identifies the dispatching group form
// so a completion cannot replace a form the user has since reopened.
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

func (groupRequest) EffectRequest() {}

func (groupRequest) effectName() string { return "group creation" }

func (value groupRequest) copied() effectRequest {
	value.fallbacks = slices.Clone(value.fallbacks)
	return value
}

func (request groupRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runGroup(request)
}

type groupEffectResult struct{ uilaunch.GroupResult }

func (groupEffectResult) EffectResult() {}

func (result groupEffectResult) apply(m *Model, job *uieffects.Job, err error) tea.Cmd {
	return m.applyGroupEffect(job.Request.(groupRequest), result, err)
}

// dispatchSpawn takes the prompt's images out of the composer and queues the
// spawn on the shared effect lane; completion routes the images.
func (m *Model) dispatchSpawn(request spawnRequest) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(spawnRequest); ok && pending.kind == request.kind && pending.composerGen == request.composerGen {
			m.reportErr("this spawn is already in progress")
			return
		}
	}
	request.id = newID()
	switch request.kind {
	case spawnForm:
		f := m.form()
		prompt := m.formPromptFor()
		request.draft = prompt.Input.Value()
		request.draftName = f.Name()
		request.draftDir = f.Dir()
		prompt.Attachments = nil
	case spawnQuick:
		q := m.quick()
		request.draft = q.Input.Value()
		q.Attachments = nil
	}
	m.enqueueEffect(request, 0, false)
}

// launchServices is the effect services as a spawn reaches them.
func (s effectServices) launchServices() uilaunch.Services {
	return uilaunch.Services{Lifecycle: s.lifecycle, Store: s.store, Config: s.cfg, Git: s.gitDrv}
}

func (s effectServices) runSpawn(request spawnRequest) (uieffects.Result, error) {
	result, err := uilaunch.Run(s.launchServices(), request.packageRequest())
	return spawnEffectResult{result}, err
}

func (s effectServices) runGroup(request groupRequest) (uieffects.Result, error) {
	result, err := uilaunch.RunGroup(s.launchServices(), uilaunch.GroupRequest{
		DraftName: request.draftName,
		DraftDir:  request.draftDir,
		Path:      request.path,
		Dir:       request.dir,
		Worktree:  request.worktree,
		Base:      request.base,
		Gen:       request.gen,
		RawDir:    request.rawDir,
		Fallbacks: request.fallbacks,
		DirReader: crossPreflight(request.dirReader),
	})
	return groupEffectResult{result}, err
}

func (m *Model) applySpawnEffect(request spawnRequest, result spawnEffectResult, err error) tea.Cmd {
	if result.RepoKnown {
		m.spawner.RememberWorktree(result.Dir, result.RepoCapable, time.Now())
	}
	if err == nil {
		sess := result.Session
		m.markFreshPane(sess.ID)
		m.lifecycle.RecordLaunch(sess.ID, time.Now())
		m.workspace.sessions = append(m.workspace.sessions, sess)
		// The directive went out with the launch, so the row waits for the
		// name the agent picks instead of showing the one generated for it.
		if request.autoNamed {
			m.lifecycle.AwaitRename(sess.ID, sess.Name, request.prompt)
		}
		if request.kind != spawnShell {
			m.spawner.RememberSpawn(request.toolName, request.pickWorktree)
			spawn.RememberModel(m.spawnHost(), request.toolName, spawn.Pick(request.choice))
		}
		// New sessions start as starting, which attention excludes; clear so
		// the row the spawn just created is on screen.
		m.rail.ClearStatusFilter()
		switch request.kind {
		case spawnForm:
			// A form opened later is a newer dialog the completion must not
			// close; only the one that dispatched this spawn is closed.
			if m.formDraftHolds(request.composerGen, request.draft, request.draftName, request.draftDir) {
				m.closeForm()
			}
		case spawnQuick:
			if q, ok := m.quickFor(request.composerGen); ok {
				m.clearAccepted(request.composerGen, request.draft, q.CloseAfterSend)
			}
		}
		m.rebuildRows()
		if request.kind == spawnShell {
			m.lifecycle.TerminalKeyAt = time.Now()
			m.focusSession(sess.ID)
		}
		if result.Label != nil {
			m.reportErr(result.Label.Error())
		}
		return m.refreshCmd()
	}
	request.plan = result.Plan
	request.dir = result.Dir
	request.worktree = result.Worktree
	if request.kind == spawnShell {
		m.lifecycle.TerminalKeyAt = time.Now()
		m.reportErr(err.Error())
		return nil
	}
	// Nothing was committed. A spawn the hint dialog refuses takes the
	// dialog's place, the same way the synchronous submit did; a dialog the
	// user opened since the dispatch is newer and keeps the screen.
	takeOver := false
	switch {
	case request.kind == spawnForm && m.formHolds(request.composerGen, request.draft):
		takeOver = true
	case request.kind == spawnQuick && m.quickHolds(request.composerGen, request.draft):
		takeOver = true
	case m.listClear():
		takeOver = true
	}
	if takeOver {
		m.reportLaunchError(err)
		if fix, ok := m.launchHintFix(); ok {
			// The dialog's images are the request's: the composer was emptied
			// at dispatch, so openLaunchHint found none of its own.
			fix.Images = append(fix.Images, request.images...)
			fix.Retry = request
			if request.kind == spawnQuick {
				m.closeQuick()
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
		if prompt, ok := m.formPromptForGen(request.composerGen); ok {
			prompt.Attachments = append(request.images, prompt.Attachments...)
			return
		}
	case spawnQuick:
		if q, ok := m.quickFor(request.composerGen); ok {
			q.Attachments = append(request.images, q.Attachments...)
			return
		}
	}
	uiinstall.DropImages(request.images)
}

func (m *Model) applyGroupEffect(request groupRequest, result groupEffectResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	// The durable inventory reconciles no matter what is on screen: the row
	// and its path/worktree are in the workspace, so the next render shows
	// them wherever the user is.
	m.materializeGroupsLocal([]string{result.Path})
	if m.workspace.groupPaths == nil {
		m.workspace.groupPaths = map[string]string{}
	}
	m.workspace.groupPaths[result.Path] = result.Dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, result.Path, result.Worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, result.Path, request.base)
	m.rebuildRows()
	// Presentation (closing the form, revealing and selecting the new group)
	// lands only when the dispatching group form is still the dialog on
	// screen; a newer one keeps the foreground. The reveal's collapse
	// snapshot queues after the row exists, the order the synchronous submit
	// kept.
	if s, ok := m.groupFormSurface(); ok && s.Gen() == request.gen && s.Name() == request.draftName && s.Dir() == request.draftDir {
		m.clearErr()
		m.Pop()
		m.applyRailStateDecision(m.rail.RevealGroup(result.Path))
	}
	return m.refreshCmd()
}

// choiceSaveRequest persists a CLI's kept choice or recent models. The
// settings cache already holds the value, so the next form reads it without
// waiting for the write.
type choiceSaveRequest struct {
	key   string
	value string
}

func (choiceSaveRequest) EffectRequest() {}

func (request choiceSaveRequest) copied() effectRequest { return request }

func (choiceSaveRequest) effectName() string { return "model choice save" }

func (request choiceSaveRequest) run(services effectServices) (uieffects.Result, error) {
	return services.runChoiceSave(request)
}

type choiceSaveResult struct{}

func (choiceSaveResult) EffectResult() {}

func (s effectServices) runChoiceSave(request choiceSaveRequest) (uieffects.Result, error) {
	return choiceSaveResult{}, uilaunch.SaveChoice(s.store, request.key, request.value)
}
