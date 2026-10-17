package ui

import (
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewPickerLoadKind uint8

const (
	reviewPickerLoadBranches reviewPickerLoadKind = iota
	reviewPickerLoadBases
)

type reviewPickerLoadRequest struct {
	kind          reviewPickerLoadKind
	source        uireview.PickerSource
	foregroundGen uint64
}

type reviewPickerLoadedMsg struct {
	request   reviewPickerLoadRequest
	rows      []uireview.PickerRow
	current   string
	storeRoot string
	err       error
}

// reviewPickerReader keeps every filesystem, git, and SQLite read behind the
// tea.Cmd boundary. Canonical deliberately returns the input on resolution
// failure, matching the existing raw-path fallback.
type reviewPickerReader interface {
	Worktrees(string) ([]git.Worktree, error)
	BranchRefs(string) ([]string, error)
	ReviewBase(string, string) (string, error)
	Canonical(string) string
}

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
	return resolveSymlinksOrSelf(root)
}

func reviewPickerReadCmd(reader reviewPickerReader, request reviewPickerLoadRequest) tea.Cmd {
	return func() tea.Msg {
		msg := reviewPickerLoadedMsg{request: request}
		switch request.kind {
		case reviewPickerLoadBranches:
			worktrees, err := reader.Worktrees(request.source.RepoRoot)
			if err != nil {
				msg.err = err
				return msg
			}
			current := reader.Canonical(request.source.RepoRoot)
			msg.rows = make([]uireview.PickerRow, len(worktrees))
			for i, worktree := range worktrees {
				msg.rows[i] = uireview.PickerRow{Label: worktree.Branch, Root: worktree.Root}
				if reader.Canonical(worktree.Root) == current {
					msg.current = worktree.Root
				}
			}
		case reviewPickerLoadBases:
			refs, err := reader.BranchRefs(request.source.RepoRoot)
			if err != nil {
				msg.err = err
				return msg
			}
			msg.storeRoot = reader.Canonical(request.source.RepoRoot)
			msg.current, msg.err = reader.ReviewBase(request.source.TargetID, msg.storeRoot)
			if msg.err != nil {
				return msg
			}
			msg.rows = make([]uireview.PickerRow, 1, len(refs)+1)
			msg.rows[0] = uireview.PickerRow{Label: "auto"}
			for _, ref := range refs {
				msg.rows = append(msg.rows, uireview.PickerRow{Label: ref, Root: ref})
			}
		}
		return msg
	}
}

func resolveSymlinksOrSelf(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func (m *Model) reviewPickerSource() uireview.PickerSource {
	state := m.review.Snapshot()
	return uireview.PickerSource{Generation: state.Generation, TargetID: state.SessionID, RepoRoot: state.RepoSelected}
}

func (m *Model) openReviewPicker(rows []uireview.PickerRow, title string, kind uireview.PickerKind, current string, source uireview.PickerSource, storeRoot string) {
	m.reviewNav.picker.Open(rows, title, kind, current, source, storeRoot)
	m.setMode(modeRepoPick)
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

func (m *Model) openBranchPickWithReader(reader reviewPickerReader) tea.Cmd {
	state := m.review.Snapshot()
	if reader == nil || state.RepoSelected == "" {
		m.reportErr("no repo under review")
		return nil
	}
	return reviewPickerReadCmd(reader, reviewPickerLoadRequest{
		kind:          reviewPickerLoadBranches,
		source:        m.reviewPickerSource(),
		foregroundGen: m.gens.foreground,
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

func (m *Model) openBasePickWithReader(reader reviewPickerReader) tea.Cmd {
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
	return reviewPickerReadCmd(reader, reviewPickerLoadRequest{
		kind:          reviewPickerLoadBases,
		source:        m.reviewPickerSource(),
		foregroundGen: m.gens.foreground,
	})
}

func (m *Model) handleReviewPickerLoaded(msg reviewPickerLoadedMsg) tea.Cmd {
	if m.effects.quitting || m.mode != modeDiff || m.gens.foreground != msg.request.foregroundGen || !m.review.PickerSourceCurrent(msg.request.source) {
		return nil
	}
	if msg.err != nil {
		m.reportErr(msg.err.Error())
		return nil
	}
	switch msg.request.kind {
	case reviewPickerLoadBranches:
		m.openReviewPicker(msg.rows, "Review branch", uireview.PickRepo, msg.current, msg.request.source, "")
	case reviewPickerLoadBases:
		m.openReviewPicker(msg.rows, "Diff base", uireview.PickBase, msg.current, msg.request.source, msg.storeRoot)
	}
	return nil
}

func (m *Model) handleRepoPickKey(msg tea.KeyMsg) tea.Cmd {
	picker := &m.reviewNav.picker
	return m.applyRepoPick(picker.Key(msg, m.review.PickerSourceCurrent(picker.Source())))
}

func (m *Model) handleRepoPickClick(line int) tea.Cmd {
	picker := &m.reviewNav.picker
	return m.applyRepoPick(picker.Click(line, m.review.PickerSourceCurrent(picker.Source())))
}

func (m *Model) applyRepoPick(result uireview.PickerResult) tea.Cmd {
	if result.Quit {
		return m.quit()
	}
	if !result.Close {
		return nil
	}
	m.setMode(modeDiff)
	if result.Error != "" {
		m.reportErr(result.Error)
	}
	if result.Choice == nil {
		return nil
	}
	if result.Choice.Kind == uireview.PickBase {
		return m.selectBase(result.Choice.Value)
	}
	return m.selectRepo(result.Choice.Value)
}

func (m *Model) viewRepoPick() string {
	picker := &m.reviewNav.picker
	body := picker.Body(m.reviewRenderContext(), m.cardWidth()-2*cardPaddingX, m.layout.height)
	return m.card(m.glyphs().ReviewGlyph+" "+picker.Title(), body, picker.Hints())
}

func (m *Model) selectRepo(root string) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok {
		m.reportErr("session is gone")
		return nil
	}
	if m.ledger.pickedRepos == nil {
		m.ledger.pickedRepos = map[string]string{}
	}
	m.ledger.pickedRepos[sess.ID] = root
	request, ok := m.review.SelectRepo(root)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}

// selectBase persists the chosen base for the current repo ("" clears to auto)
// then reloads. It forces the branch scope so the freshly picked base is what
// the review actually shows.
func (m *Model) selectBase(ref string) tea.Cmd {
	if m.reviewBaseSavePending() {
		m.reportErr("diff base is still saving")
		return nil
	}
	source := m.reviewNav.picker.Source()
	if !m.review.PickerSourceCurrent(source) {
		m.reportErr("review changed; reopen the picker")
		return nil
	}
	sess, ok := m.diffSession()
	if !ok || sess.ID != source.TargetID {
		m.reportErr("session is gone")
		return nil
	}
	storeRoot := m.reviewNav.picker.StoreRoot()
	if storeRoot == "" {
		m.reportErr("review repository is gone")
		return nil
	}
	m.enqueueEffect(reviewEffectRequest{
		op: reviewOpSetBase, targetID: sess.ID, repoRoot: storeRoot,
		sourceRepo: source.RepoRoot, generation: source.Generation, baseRef: ref,
	}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) reviewBaseSavePending() bool {
	isBaseSave := func(job *effectJob) bool {
		if job == nil {
			return false
		}
		request, ok := job.request.(reviewEffectRequest)
		return ok && request.op == reviewOpSetBase
	}
	if isBaseSave(m.effects.main.active) {
		return true
	}
	for _, job := range m.effects.main.pending {
		if isBaseSave(job) {
			return true
		}
	}
	return false
}

func (m *Model) applyReviewBase(result reviewBaseResult) tea.Cmd {
	source := uireview.PickerSource{Generation: result.generation, TargetID: result.targetID, RepoRoot: result.sourceRepo}
	if result.err != nil {
		if !m.effects.quitting {
			m.reportErr(result.err.Error())
		}
		return nil
	}
	if m.effects.quitting || !m.review.PickerSourceCurrent(source) {
		return nil
	}
	request, ok := m.review.SelectBase(result.ref)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}
