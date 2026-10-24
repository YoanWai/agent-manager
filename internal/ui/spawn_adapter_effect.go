package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

// dispatchSpawn takes the prompt's images out of the composer and queues the
// spawn on the shared effect lane; completion routes the images.
func (m *Model) dispatchSpawn(request uilaunch.Request) {
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if pending, ok := job.Request.(uilaunch.Request); ok && pending.Kind == request.Kind && pending.ComposerGen == request.ComposerGen {
			m.reportErr("this spawn is already in progress")
			return
		}
	}
	request.ID = newID()
	switch request.Kind {
	case uilaunch.Form:
		f := m.form()
		prompt := m.formPromptFor()
		request.Draft = prompt.Input.Value()
		request.DraftName = f.Name()
		request.DraftDir = f.Dir()
		prompt.Attachments = nil
	case uilaunch.Quick:
		q := m.quick()
		request.Draft = q.Input.Value()
		q.Attachments = nil
	}
	m.enqueueEffect(request, 0, false)
}

// applySpawn applies a finished spawn: a committed row joins the workspace,
// and a refused one hands its prompt back to the dialog that still shows it.
func (m *Model) applySpawn(request uilaunch.Request, result uilaunch.Result, err error) tea.Cmd {
	if result.RepoKnown {
		m.ledger.spawner.RememberWorktree(result.Dir, result.RepoCapable, time.Now())
	}
	if err == nil {
		m.commitSpawn(request, result.Session)
		if result.Label != nil {
			m.reportErr(result.Label.Error())
		}
		return m.refreshCmd()
	}
	request.Plan = result.Plan
	request.Dir = result.Dir
	request.Worktree = result.Worktree
	if request.Kind == uilaunch.Shell {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		m.reportErr(err.Error())
		return nil
	}
	// Nothing was committed. A spawn the hint dialog refuses takes the
	// dialog's place, the same way the synchronous submit did; a dialog the
	// user opened since the dispatch is newer and keeps the screen.
	if !m.spawnTakesOver(request) {
		m.returnSpawnImages(request)
		m.reportErr(err.Error())
		return nil
	}
	m.reportLaunchError(err)
	if fix, ok := m.launchHintFix(); ok {
		// The dialog's images are the request's: the composer was emptied at
		// dispatch, so openLaunchHint found none of its own.
		fix.Images = append(fix.Images, request.Images...)
		fix.Retry = request
		if request.Kind == uilaunch.Quick {
			m.closeQuick()
		}
	} else {
		// A plain error left the composer on screen; its images go back.
		m.returnSpawnImages(request)
	}
	return nil
}

func (m *Model) commitSpawn(request uilaunch.Request, sess store.Session) {
	m.markFreshPane(sess.ID)
	m.ledger.lifecycle.RecordLaunch(sess.ID, time.Now())
	m.workspace.sessions = append(m.workspace.sessions, sess)
	// The directive went out with the launch, so the row waits for the name
	// the agent picks instead of showing the one generated for it.
	if request.AutoNamed {
		m.ledger.lifecycle.AwaitRename(sess.ID, sess.Name, request.Prompt)
	}
	if request.Kind != uilaunch.Shell {
		m.ledger.spawner.RememberSpawn(request.ToolName, request.PickWorktree)
		spawn.RememberModel(m.spawnHost(), request.ToolName, spawn.Pick(request.Choice))
	}
	// New sessions start as starting, which attention excludes; clear so the
	// row the spawn just created is on screen.
	m.rail.ClearStatusFilter()
	switch request.Kind {
	case uilaunch.Form:
		// A form opened later is a newer dialog the completion must not
		// close; only the one that dispatched this spawn is closed.
		if m.formDraftHolds(request.ComposerGen, request.Draft, request.DraftName, request.DraftDir) {
			m.closeForm()
		}
	case uilaunch.Quick:
		if q, ok := m.quickFor(request.ComposerGen); ok {
			m.clearAccepted(request.ComposerGen, request.Draft, q.CloseAfterSend)
		}
	}
	m.rebuildRows()
	if request.Kind == uilaunch.Shell {
		m.ledger.lifecycle.TerminalKeyAt = time.Now()
		m.focusSession(sess.ID)
	}
}

// spawnTakesOver reports whether a refused spawn may replace the screen: the
// dialog that dispatched it still shows its prompt, or the list is bare.
func (m *Model) spawnTakesOver(request uilaunch.Request) bool {
	switch {
	case request.Kind == uilaunch.Form && m.formHolds(request.ComposerGen, request.Draft):
		return true
	case request.Kind == uilaunch.Quick && m.quickHolds(request.ComposerGen, request.Draft):
		return true
	}
	return m.listClear()
}

// returnSpawnImages gives the prompt's images back to the composer that is
// still on screen, so the user can fix and resubmit; when no such composer
// remains, the files are dropped.
func (m *Model) returnSpawnImages(request uilaunch.Request) {
	switch request.Kind {
	case uilaunch.Form:
		if prompt, ok := m.formPromptForGen(request.ComposerGen); ok {
			prompt.Attachments = append(request.Images, prompt.Attachments...)
			return
		}
	case uilaunch.Quick:
		if q, ok := m.quickFor(request.ComposerGen); ok {
			q.Attachments = append(request.Images, q.Attachments...)
			return
		}
	}
	uiinstall.DropImages(request.Images)
}

// applyGroupEffect applies a created group: the durable inventory takes the
// row wherever the user is, and only the form that dispatched it gives up
// the screen.
func (m *Model) applyGroupEffect(request uilaunch.GroupRequest, result uilaunch.GroupResult, err error) tea.Cmd {
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	m.materializeGroupsLocal([]string{result.Path})
	if m.workspace.groupPaths == nil {
		m.workspace.groupPaths = map[string]string{}
	}
	m.workspace.groupPaths[result.Path] = result.Dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, result.Path, result.Worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, result.Path, request.Base)
	m.rebuildRows()
	// Presentation (closing the form, revealing and selecting the new group)
	// lands only when the dispatching group form is still the dialog on
	// screen; a newer one keeps the foreground. The reveal's collapse
	// snapshot queues after the row exists, the order the synchronous submit
	// kept.
	if s, ok := surfaceOf[*uidialogs.GroupFormSurface](m); ok && s.Gen() == request.Gen && s.Name() == request.DraftName && s.Dir() == request.DraftDir {
		m.clearErr()
		m.Pop()
		m.queueRail(m.rail.RevealGroup(result.Path).Mutations, 0, true)
	}
	return m.refreshCmd()
}

// launchServices is the effect services as a spawn reaches them.
func (s effectServices) launchServices() uilaunch.Services {
	return uilaunch.Services{Lifecycle: s.lifecycle, Store: s.store, Config: s.cfg, Git: s.gitDrv}
}
