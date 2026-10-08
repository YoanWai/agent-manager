package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickKind selects what a picker's Enter does: retarget the repo/branch, or
// set the diff base.
type pickKind int

const (
	pickRepo pickKind = iota
	pickBase
)

// pickRow is one selectable target: label is what the user reads (a repo base
// name, a worktree's branch, or a base ref), root carries the path selectRepo
// retargets to, or the ref selectBase persists ("" for auto).
type pickRow struct {
	label string
	root  string
}

// rows is snapshotted at open because a refresh landing behind the picker would reorder rows under the cursor.
type repoPickState struct {
	rows      []pickRow
	filter    string
	cursor    int
	title     string
	kind      pickKind
	source    reviewPickerSource
	storeRoot string
}

func (m *Model) openRepoPick() {
	state := m.review.Snapshot()
	if len(state.RepoRoots) == 0 {
		return
	}
	rows := make([]pickRow, len(state.RepoRoots))
	for i, root := range state.RepoRoots {
		rows[i] = pickRow{label: filepath.Base(root), root: root}
	}
	m.openPick(rows, "⌥ Review repo", pickRepo, state.RepoSelected, reviewPickerSource{
		generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
	}, "")
}

// openBranchPick lists the currently selected repo's worktrees, one branch per
// row, so the user can retarget review to another worktree.
func (m *Model) openBranchPick() tea.Cmd {
	if m.services.gitDrv == nil {
		m.errBar.text = "no repo under review"
		return nil
	}
	return m.openBranchPickWithReader(systemReviewPickerReader{git: m.services.gitDrv, store: m.services.store})
}

func (m *Model) openBranchPickWithReader(reader reviewPickerReader) tea.Cmd {
	state := m.review.Snapshot()
	if reader == nil || state.RepoSelected == "" {
		m.errBar.text = "no repo under review"
		return nil
	}
	return reviewPickerReadCmd(reader, reviewPickerLoadRequest{
		kind: reviewPickerLoadBranches,
		source: reviewPickerSource{
			generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
		},
		foregroundGen: m.gens.foreground,
	})
}

// openBasePick lists auto plus the current repo's branch refs so the user can
// override the base the branch scope diffs against, cursor on the stored base.
func (m *Model) openBasePick() tea.Cmd {
	if m.services.gitDrv == nil || m.services.store == nil {
		m.errBar.text = "no repo under review"
		return nil
	}
	return m.openBasePickWithReader(systemReviewPickerReader{git: m.services.gitDrv, store: m.services.store})
}

func (m *Model) openBasePickWithReader(reader reviewPickerReader) tea.Cmd {
	if m.reviewBaseSavePending() {
		m.errBar.text = "diff base is still saving"
		return nil
	}
	// Key off the raw selection, not the resolved toplevel: the toplevel is
	// empty after a bad base errors the load, which would make the one control
	// that clears the bad base unreachable exactly when it is needed.
	state := m.review.Snapshot()
	if reader == nil || state.RepoSelected == "" {
		m.errBar.text = "no repo under review"
		return nil
	}
	return reviewPickerReadCmd(reader, reviewPickerLoadRequest{
		kind: reviewPickerLoadBases,
		source: reviewPickerSource{
			generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
		},
		foregroundGen: m.gens.foreground,
	})
}

func (m *Model) handleReviewPickerLoaded(msg reviewPickerLoadedMsg) tea.Cmd {
	if m.effects.quitting || m.mode != modeDiff || m.gens.foreground != msg.request.foregroundGen || !m.reviewPickerSourceCurrent(msg.request.source) {
		return nil
	}
	if msg.err != nil {
		m.errBar.text = msg.err.Error()
		return nil
	}
	switch msg.request.kind {
	case reviewPickerLoadBranches:
		m.openPick(msg.rows, "⌥ Review branch", pickRepo, msg.current, msg.request.source, "")
	case reviewPickerLoadBases:
		m.openPick(msg.rows, "⌥ Diff base", pickBase, msg.current, msg.request.source, msg.storeRoot)
	}
	return nil
}

func (m *Model) openPick(rows []pickRow, title string, kind pickKind, current string, source reviewPickerSource, storeRoot string) {
	m.reviewNav.picker = repoPickState{rows: rows, title: title, kind: kind, source: source, storeRoot: storeRoot}
	for i, row := range rows {
		if row.root == current {
			m.reviewNav.picker.cursor = i
			break
		}
	}
	m.mode = modeRepoPick
	m.errBar.text = ""
}

func resolveSymlinksOrSelf(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func (m *Model) filteredRows() []pickRow {
	if m.reviewNav.picker.filter == "" {
		return m.reviewNav.picker.rows
	}
	needle := strings.ToLower(m.reviewNav.picker.filter)
	var out []pickRow
	for _, row := range m.reviewNav.picker.rows {
		if strings.Contains(strings.ToLower(row.label), needle) ||
			strings.Contains(strings.ToLower(row.root), needle) {
			out = append(out, row)
		}
	}
	return out
}

func (m *Model) handleRepoPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.filteredRows()
	switch msg.Type {
	case tea.KeyCtrlC:
		return m.requestQuit()
	case tea.KeyEsc:
		m.mode = modeDiff
		return m, nil
	case tea.KeyUp:
		m.moveRepoPickCursor(-1, len(rows))
		return m, nil
	case tea.KeyDown:
		m.moveRepoPickCursor(1, len(rows))
		return m, nil
	case tea.KeyBackspace:
		if m.reviewNav.picker.filter != "" {
			m.reviewNav.picker.filter = m.reviewNav.picker.filter[:len(m.reviewNav.picker.filter)-1]
			m.reviewNav.picker.cursor = 0
		}
		return m, nil
	case tea.KeyEnter:
		if len(rows) == 0 {
			return m, nil
		}
		if !m.reviewPickerSourceCurrent(m.reviewNav.picker.source) {
			m.mode = modeDiff
			m.errBar.text = "review changed; reopen the picker"
			return m, nil
		}
		m.mode = modeDiff
		row := rows[m.reviewNav.picker.cursor]
		if m.reviewNav.picker.kind == pickBase {
			return m, m.selectBase(row.root)
		}
		return m, m.selectRepo(row.root)
	case tea.KeyRunes:
		m.reviewNav.picker.filter += string(msg.Runes)
		m.reviewNav.picker.cursor = 0
		return m, nil
	}
	return m, nil
}

func (m *Model) moveRepoPickCursor(delta, count int) {
	if count == 0 {
		m.reviewNav.picker.cursor = 0
		return
	}
	m.reviewNav.picker.cursor = (m.reviewNav.picker.cursor + delta + count) % count
}

func (m *Model) selectRepo(root string) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok {
		m.errBar.text = "session is gone"
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
		m.errBar.text = "diff base is still saving"
		return nil
	}
	if !m.reviewPickerSourceCurrent(m.reviewNav.picker.source) {
		m.errBar.text = "review changed; reopen the picker"
		return nil
	}
	sess, ok := m.diffSession()
	if !ok || sess.ID != m.reviewNav.picker.source.targetID {
		m.errBar.text = "session is gone"
		return nil
	}
	if m.reviewNav.picker.storeRoot == "" {
		m.errBar.text = "review repository is gone"
		return nil
	}
	m.enqueueEffect(reviewEffectRequest{
		op: reviewOpSetBase, targetID: sess.ID, repoRoot: m.reviewNav.picker.storeRoot,
		sourceRepo: m.reviewNav.picker.source.repoRoot, generation: m.reviewNav.picker.source.generation, baseRef: ref,
	}, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) reviewPickerSourceCurrent(source reviewPickerSource) bool {
	state := m.review.Snapshot()
	return state.Active && state.Generation == source.generation && state.SessionID == source.targetID && state.RepoSelected == source.repoRoot
}

func (m *Model) reviewBaseSavePending() bool {
	isBaseSave := func(job *effectJob) bool {
		if job == nil {
			return false
		}
		request, ok := job.request.(reviewEffectRequest)
		return ok && request.op == reviewOpSetBase
	}
	if isBaseSave(m.effects.active) {
		return true
	}
	for _, job := range m.effects.pending {
		if isBaseSave(job) {
			return true
		}
	}
	return false
}

func (m *Model) applyReviewBase(result reviewBaseResult) tea.Cmd {
	source := reviewPickerSource{generation: result.generation, targetID: result.targetID, repoRoot: result.sourceRepo}
	if result.err != nil {
		if !m.effects.quitting {
			m.errBar.text = result.err.Error()
		}
		return nil
	}
	if m.effects.quitting || !m.reviewPickerSourceCurrent(source) {
		return nil
	}
	request, ok := m.review.SelectBase(result.ref)
	if !ok {
		return nil
	}
	return m.reviewLoadCmd(request)
}

func (m *Model) repoPickWindow(count int) (start, end int) {
	visible := max(3, m.layout.height-repoPickChrome)
	if count <= visible {
		return 0, count
	}
	start = m.reviewNav.picker.cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > count {
		start = count - visible
	}
	return start, start + visible
}

// Card lines around the rows: border, padding, title, spacers, error, hint, "+N more".
const repoPickChrome = 12

func (m *Model) repoPickRow(row pickRow, selected bool) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle()
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		nameStyle = nameStyle.Foreground(colorAccent).Bold(true)
	}
	inner := m.cardWidth() - 2*cardPaddingX
	name := truncateTail(escapeControlsInline(row.label), inner-lipgloss.Width(marker))
	budget := inner - lipgloss.Width(marker) - lipgloss.Width(name) - 2
	dir := ""
	if budget > 1 {
		dir = subtleStyle.Render("  " + truncateTail(escapeControlsInline(filepath.Dir(row.root)), budget))
	}
	return marker + nameStyle.Render(name) + dir
}

func (m *Model) viewRepoPick() string {
	rows := m.filteredRows()
	var body strings.Builder
	body.WriteString(mutedStyle.Render("filter: ") + m.reviewNav.picker.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no match"))
	}
	start, end := m.repoPickWindow(len(rows))
	for i := start; i < end; i++ {
		body.WriteString(m.repoPickRow(rows[i], i == m.reviewNav.picker.cursor) + "\n")
	}
	if hidden := len(rows) - (end - start); hidden > 0 {
		body.WriteString(subtleStyle.Render(fmt.Sprintf("+%d more", hidden)) + "\n")
	}
	return m.card(m.reviewNav.picker.title, strings.TrimRight(body.String(), "\n"), [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵", "select"}, {"esc", "cancel"}})
}
