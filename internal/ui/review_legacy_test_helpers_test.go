package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) reviewKey() string {
	state := m.review.Snapshot()
	return state.SessionID + "\x00" + state.RepoSelected
}

func (m *Model) reviewedMarkKey(path string) string {
	return m.review.Snapshot().Scope.String() + "\x00" + path
}

func (m *Model) openAnnotate() { m.review.OpenAnnotation() }

func (m *Model) saveAnnotation() tea.Cmd {
	result := m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyEnter})
	return m.reviewCommands(result.Requests)
}

func (m *Model) sendAnnotations() (tea.Model, tea.Cmd) {
	result := m.review.BeginSend()
	if result.Error != "" {
		m.errBar.text = result.Error
	}
	return m, m.reviewCommands(result.Requests)
}

func (m *Model) discardOrToggleAnnotation() tea.Cmd {
	requests, errText := m.review.DiscardOrToggle(m.services.store != nil)
	if errText != "" {
		m.errBar.text = errText
	}
	return m.reviewCommands(requests)
}

func (m *Model) toggleReviewed() tea.Cmd          { return m.reviewCommands(m.review.ToggleReviewed()) }
func (m *Model) toggleCodeOnly() tea.Cmd          { return m.reviewCommands(m.review.ToggleCodeOnly()) }
func (m *Model) fileReviewed(path string) bool    { return m.review.FileReviewed(path) }
func (m *Model) moveDiffCursor(delta, height int) { m.review.MoveCursor(delta, height) }
func (m *Model) jumpChange(delta int)             { m.review.JumpChange(delta, m.diffCodeHeight()) }
func (m *Model) setCursorDiffLine(line int)       { m.review.SetCursorDiffLine(line, m.diffCodeHeight()) }
func (m *Model) switchDiffFile(delta int) tea.Cmd {
	return m.reviewCommands(m.review.SwitchFile(delta))
}

func (m *Model) fileDiffByPath(path string) *diff.FileDiff {
	for _, fd := range m.review.SetCopy().Files {
		if fd.File.Path == path {
			copy := fd
			return &copy
		}
	}
	return nil
}

func (m *Model) diffLoadCmd(sess store.Session, scope git.Scope, generation int, repo string, refresh bool) tea.Cmd {
	state := m.review.Snapshot()
	return m.reviewLoadCmd(uireview.LoadRequest{
		Target: reviewTarget(sess), Scope: scope, Generation: generation,
		RepoWanted: repo, Refresh: refresh, Resolve: true,
		Restored: map[string]bool{sess.ID + "\x00" + state.RepoSelected: len(m.review.SavedState().Comments) > 0 || len(m.review.SavedState().Reviewed) > 0},
	})
}

func (m *Model) diffProbeCmd(sess store.Session, scope git.Scope) tea.Cmd {
	state := m.review.Snapshot()
	return m.reviewProbeCmd(uireview.ProbeRequest{Target: reviewTarget(sess), Scope: scope, RepoSelected: state.RepoSelected, GitRoot: state.Set.Repo.Root})
}

func seedReviewForTest(m *Model, target uireview.Target, scope git.Scope, repo string, set diff.Set, apply bool) uireview.LoadRequest {
	m.review = uireview.New(false)
	request := m.review.Open(target, scope, repo)
	if apply {
		m.review.ApplyLoad(uireview.LoadResult{
			TargetID: target.ID, Scope: scope, Generation: request.Generation,
			RepoRoot: repo, RepoRoots: []string{repo}, Set: set,
		})
	}
	return request
}

func seedReviewStateForTest(m *Model, target uireview.Target, scope git.Scope, repo string, set diff.Set, state uireview.SavedState) uireview.LoadRequest {
	m.review = uireview.New(false)
	request := m.review.Open(target, scope, repo)
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: target.ID, Scope: scope, Generation: request.Generation,
		RepoRoot: repo, RepoRoots: []string{repo}, Set: set,
		Saved: state, SavedLoaded: true,
	})
	return request
}

func setReviewSideBySide(m *Model, split bool) {
	if m.review.Snapshot().SideBySide != split {
		m.review.ToggleSideBySide(m.diffCodeHeight())
	}
}

func selectReviewFile(m *Model, index int) tea.Cmd {
	state := m.review.Snapshot()
	return m.reviewCommands(m.review.SwitchFile(index - state.FileIndex))
}

func typeReviewAnnotation(m *Model, text string) {
	m.review.AnnotationKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func replaceReviewRootsForTest(m *Model, roots []string, selected string) {
	state := m.review.Snapshot()
	m.review.ApplyLoad(uireview.LoadResult{
		TargetID: state.SessionID, Scope: state.Scope, Generation: state.Generation,
		RepoRoot: selected, RepoRoots: append([]string(nil), roots...),
		Set: m.review.SetCopy(), Fingerprint: state.Fingerprint,
	})
}
