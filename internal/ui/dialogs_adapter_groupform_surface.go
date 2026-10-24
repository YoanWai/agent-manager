package ui

import (
	"strings"

	uilink "github.com/YoanWai/agent-manager/internal/ui/connections/link"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

// groupRoot is the root as the New Group card reaches it.
type groupRoot struct{ *Model }

func (r groupRoot) CardWidth(cols int) int { return uiframe.CardWidthFor(cols) }

func (r groupRoot) ReportErr(text string) { r.reportErr(text) }

var _ uidialogs.GroupFormRoot = groupRoot{}

// openGroupForm opens the New Group card, seeded with the parent picker and
// what each parent lends a child.
func (m *Model) openGroupForm() {
	opts := uidialogs.FormOptions{
		Gen:       m.nextComposerGen(),
		Cols:      m.layout.width,
		Worktrees: groupWorktreeOptions,
		Styles:    m.spawnStyles(),
	}
	if target, ok := m.remoteTarget(); ok {
		opts.Remote = uidialogs.Remote{Host: target.host, Group: target.group}
		m.push(uidialogs.NewGroupFormSurface(groupRoot{m}, opts))
		m.clearErr()
		return
	}
	parents, at := m.groupOptions(m.contextGroup())
	lends := make(map[string]uidialogs.Lend, len(parents))
	for _, opt := range parents {
		lends[opt.Path] = uidialogs.Lend{Dir: m.capturedAncestorGroupDir(opt.Path), Base: m.groupBase(opt.Path)}
	}
	opts.Dir = m.capturedGroupDefaultDir(parents[at].Path)
	opts.Parents, opts.Parent, opts.Lends = parents, at, lends
	m.push(uidialogs.NewGroupFormSurface(groupRoot{m}, opts))
	m.clearErr()
}

// handleGroupFormSubmit runs what the form's submit asked: a remote call
// for a connection's group, otherwise the store write on the effect lane.
func (m *Model) handleGroupFormSubmit(msg uidialogs.GroupFormSubmitMsg, reader directoryPreflight) tea.Cmd {
	request := msg.Request
	m.clearErr()
	if target := msg.Remote; target.Host != "" {
		return m.queueRemote(remoteRequest{op: uilink.CreateGroup, host: target.Host, name: request.Path,
			groupDir: strings.TrimSpace(request.Dir), composerGen: request.Gen})
	}
	m.enqueueEffect(groupRequest{
		path: request.Path, worktree: groupWorktreeValue(request.Worktree), base: msg.Base, gen: request.Gen,
		rawDir: request.Dir, fallbacks: m.groupDirCandidates(request.Parent), dirReader: reader,
		draftName: request.Name, draftDir: request.Dir,
	}, 0, false)
	return m.nextEffectCmd()
}

// handleGroupBaseAsk steps the open form's base through the repo's
// branches, probing the directory the group would save.
func (m *Model) handleGroupBaseAsk(msg uidialogs.GroupBaseAskMsg) tea.Cmd {
	s, ok := surfaceOf[*uidialogs.GroupFormSurface](m)
	if !ok || s.Gen() != msg.Gen {
		return nil
	}
	choice, cmd := m.stepGroupBase(uilaunch.BaseForm, uint64(msg.Gen), m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(s.Selected())), s.Base(), msg.Delta)
	*s.BaseRef() = choice
	return cmd
}
