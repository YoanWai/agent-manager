package ui

import (
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	tea "github.com/charmbracelet/bubbletea"
)

// systemReviewPickerReader reads picker rows from the real git driver and
// store.
type systemReviewPickerReader struct {
	git   *git.Driver
	store *store.Store
}

func (r systemReviewPickerReader) Worktrees(root string) ([]git.Worktree, error) {
	return r.git.Worktrees(root)
}

func (r systemReviewPickerReader) BranchRefs(root string) ([]string, error) {
	return r.git.BranchRefs(root)
}

func (r systemReviewPickerReader) ReviewBase(targetID, root string) (string, error) {
	return r.store.ReviewBase(targetID, root)
}

func (systemReviewPickerReader) Canonical(root string) string {
	return uireviewsource.Canonical(root)
}

func (m *Model) reviewPickerSource() uireview.PickerSource {
	state := m.review.Snapshot()
	return uireview.PickerSource{Generation: state.Generation, TargetID: state.SessionID, RepoRoot: state.RepoSelected}
}

func (m *Model) openReviewPicker(rows []uireview.PickerRow, title string, kind uireview.PickerKind, current string, source uireview.PickerSource, storeRoot string) {
	m.push(uireview.NewSurface(reviewRoot{m}, rows, title, kind, current, source, storeRoot, m.reviewRenderContext(), m.glyphs().ReviewGlyph))
	m.clearErr()
}

func (m *Model) openRepoPick() {
	state := m.review.Snapshot()
	if len(state.RepoRoots) == 0 {
		return
	}
	rows := make([]uireview.PickerRow, len(state.RepoRoots))
	for i, root := range state.RepoRoots {
		rows[i] = uireview.PickerRow{Label: filepath.Base(root), Root: root}
	}
	m.openReviewPicker(rows, "Review repo", uireview.PickRepo, state.RepoSelected, m.reviewPickerSource(), "")
}

// openBranchPick lists the currently selected repo's worktrees, one branch per
// row, so the user can retarget review to another worktree.
func (m *Model) openBranchPick() tea.Cmd {
	if m.services.gitDrv == nil {
		m.reportErr("no repo under review")
		return nil
	}
	return m.openBranchPickWithReader(systemReviewPickerReader{git: m.services.gitDrv, store: m.services.store})
}

func (m *Model) openBranchPickWithReader(reader uireviewsource.PickerReader) tea.Cmd {
	state := m.review.Snapshot()
	if reader == nil || state.RepoSelected == "" {
		m.reportErr("no repo under review")
		return nil
	}
	return uireviewsource.PickerReadCmd(reader, uireviewsource.PickerLoadRequest{
		Kind:          uireviewsource.PickerLoadBranches,
		Source:        m.reviewPickerSource(),
		ForegroundGen: m.gens.foreground,
	})
}

// openBasePick lists auto plus the current repo's branch refs so the user can
// override the base the branch scope diffs against, cursor on the stored base.
func (m *Model) openBasePick() tea.Cmd {
	if m.services.gitDrv == nil || m.services.store == nil {
		m.reportErr("no repo under review")
		return nil
	}
	return m.openBasePickWithReader(systemReviewPickerReader{git: m.services.gitDrv, store: m.services.store})
}

func (m *Model) openBasePickWithReader(reader uireviewsource.PickerReader) tea.Cmd {
	if m.reviewBaseSavePending() {
		m.reportErr("diff base is still saving")
		return nil
	}
	// Key off the raw selection, not the resolved toplevel: the toplevel is
	// empty after a bad base errors the load, which would make the one control
	// that clears the bad base unreachable exactly when it is needed.
	state := m.review.Snapshot()
	if reader == nil || state.RepoSelected == "" {
		m.reportErr("no repo under review")
		return nil
	}
	return uireviewsource.PickerReadCmd(reader, uireviewsource.PickerLoadRequest{
		Kind:          uireviewsource.PickerLoadBases,
		Source:        m.reviewPickerSource(),
		ForegroundGen: m.gens.foreground,
	})
}

func (m *Model) handleReviewPickerLoaded(msg uireviewsource.PickerLoadedMsg) tea.Cmd {
	if m.effects.Quitting || m.base != baseReview || m.gens.foreground != msg.Request.ForegroundGen || !m.review.PickerSourceCurrent(msg.Request.Source) {
		return nil
	}
	if msg.Err != nil {
		m.reportErr(msg.Err.Error())
		return nil
	}
	switch msg.Request.Kind {
	case uireviewsource.PickerLoadBranches:
		m.openReviewPicker(msg.Rows, "Review branch", uireview.PickRepo, msg.Current, msg.Request.Source, "")
	case uireviewsource.PickerLoadBases:
		m.openReviewPicker(msg.Rows, "Diff base", uireview.PickBase, msg.Current, msg.Request.Source, msg.StoreRoot)
	}
	return nil
}

// applyReviewPick acts on a pick once its surface has closed. The picker
// snapshotted a review; a load or a base save that landed behind it moved
// the review on, so a pick for the old one is refused where the user can
// see why, instead of retargeting whatever is showing now.
func (m *Model) applyReviewPick(msg uireview.PickedMsg) tea.Cmd {
	if m.effects.Quitting || m.base != baseReview {
		return nil
	}
	if !m.review.PickerSourceCurrent(msg.Source) {
		m.reportErr("review changed; reopen the picker")
		return nil
	}
	if msg.Choice.Kind == uireview.PickBase {
		return m.selectBase(msg.Choice.Value, msg.Source, msg.StoreRoot)
	}
	return m.selectRepo(msg.Choice.Value)
}

func (m *Model) selectRepo(root string) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok {
		m.reportErr("session is gone")
		return nil
	}
	if m.ledger.lifecycle.PickedRepos == nil {
		m.ledger.lifecycle.PickedRepos = map[string]string{}
	}
	m.ledger.lifecycle.PickedRepos[sess.ID] = root
	request, ok := m.review.SelectRepo(root)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}

// selectBase persists the chosen base for the current repo ("" clears to auto)
// then reloads. It forces the branch scope so the freshly picked base is what
// the review actually shows.
func (m *Model) selectBase(ref string, source uireview.PickerSource, storeRoot string) tea.Cmd {
	if m.reviewBaseSavePending() {
		m.reportErr("diff base is still saving")
		return nil
	}
	sess, ok := m.diffSession()
	if !ok || sess.ID != source.TargetID {
		m.reportErr("session is gone")
		return nil
	}
	if storeRoot == "" {
		m.reportErr("review repository is gone")
		return nil
	}
	m.enqueueEffect(uireviewsource.Change{Request: uireviewsource.Request{
		Op: uireviewsource.SetBase, TargetID: sess.ID, RepoRoot: storeRoot,
		SourceRepo: source.RepoRoot, Generation: source.Generation, BaseRef: ref,
	}}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) reviewBaseSavePending() bool {
	return uireviewsource.PendingSetBase(append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...)...)
}

func (m *Model) applyReviewBase(result uireviewsource.BaseResult) tea.Cmd {
	source := uireview.PickerSource{Generation: result.Generation, TargetID: result.TargetID, RepoRoot: result.SourceRepo}
	if result.Err != nil {
		if !m.effects.Quitting {
			m.reportErr(result.Err.Error())
		}
		return nil
	}
	if m.effects.Quitting || !m.review.PickerSourceCurrent(source) {
		return nil
	}
	request, ok := m.review.SelectBase(result.Ref)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}
