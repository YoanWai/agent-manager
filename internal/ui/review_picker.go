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

// repoPicker is the review's repo, branch and base picker: the rows
// snapshotted at open, the filter and the cursor. Reading the rows and acting
// on a choice are root adapters (Git, SQLite, the effect lane), reached
// through repoPickerHost.
type repoPicker struct{ repoPickState }

type repoPickerHost interface {
	setMode(mode)
	clearErr()
	reportErr(string)
	requestQuit() (tea.Model, tea.Cmd)
	reviewPickerSourceCurrent(reviewPickerSource) bool
	selectRepo(root string) tea.Cmd
	selectBase(ref string) tea.Cmd
}

// repoPickerViewHost is the dialog chrome the picker paints into.
type repoPickerViewHost interface {
	size() (width, height int)
	cardWidth() int
	card(title, body string, hint [][2]string) string
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
	m.reviewNav.picker.open(m, rows, "⌥ Review repo", pickRepo, state.RepoSelected, reviewPickerSource{
		generation: state.Generation, targetID: state.SessionID, repoRoot: state.RepoSelected,
	}, "")
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
		m.reportErr(msg.err.Error())
		return nil
	}
	switch msg.request.kind {
	case reviewPickerLoadBranches:
		m.reviewNav.picker.open(m, msg.rows, "⌥ Review branch", pickRepo, msg.current, msg.request.source, "")
	case reviewPickerLoadBases:
		m.reviewNav.picker.open(m, msg.rows, "⌥ Diff base", pickBase, msg.current, msg.request.source, msg.storeRoot)
	}
	return nil
}

func (p *repoPicker) open(h repoPickerHost, rows []pickRow, title string, kind pickKind, current string, source reviewPickerSource, storeRoot string) {
	p.repoPickState = repoPickState{rows: rows, title: title, kind: kind, source: source, storeRoot: storeRoot}
	for i, row := range rows {
		if row.root == current {
			p.cursor = i
			break
		}
	}
	h.setMode(modeRepoPick)
	h.clearErr()
}

func resolveSymlinksOrSelf(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path
	}
	return resolved
}

func (p *repoPicker) filteredRows() []pickRow {
	if p.filter == "" {
		return p.rows
	}
	needle := strings.ToLower(p.filter)
	var out []pickRow
	for _, row := range p.rows {
		if strings.Contains(strings.ToLower(row.label), needle) ||
			strings.Contains(strings.ToLower(row.root), needle) {
			out = append(out, row)
		}
	}
	return out
}

func (p *repoPicker) handleKey(h repoPickerHost, msg tea.KeyMsg) tea.Cmd {
	rows := p.filteredRows()
	switch msg.Type {
	case tea.KeyCtrlC:
		_, cmd := h.requestQuit()
		return cmd
	case tea.KeyEsc:
		h.setMode(modeDiff)
		return nil
	case tea.KeyUp:
		p.moveCursor(-1, len(rows))
		return nil
	case tea.KeyDown:
		p.moveCursor(1, len(rows))
		return nil
	case tea.KeyBackspace:
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.cursor = 0
		}
		return nil
	case tea.KeyEnter:
		if len(rows) == 0 {
			return nil
		}
		if !h.reviewPickerSourceCurrent(p.source) {
			h.setMode(modeDiff)
			h.reportErr("review changed; reopen the picker")
			return nil
		}
		h.setMode(modeDiff)
		row := rows[p.cursor]
		if p.kind == pickBase {
			return h.selectBase(row.root)
		}
		return h.selectRepo(row.root)
	case tea.KeyRunes:
		p.filter += string(msg.Runes)
		p.cursor = 0
		return nil
	}
	return nil
}

func (p *repoPicker) moveCursor(delta, count int) {
	if count == 0 {
		p.cursor = 0
		return
	}
	p.cursor = (p.cursor + delta + count) % count
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
	if !m.reviewPickerSourceCurrent(m.reviewNav.picker.source) {
		m.reportErr("review changed; reopen the picker")
		return nil
	}
	sess, ok := m.diffSession()
	if !ok || sess.ID != m.reviewNav.picker.source.targetID {
		m.reportErr("session is gone")
		return nil
	}
	if m.reviewNav.picker.storeRoot == "" {
		m.reportErr("review repository is gone")
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
			m.reportErr(result.err.Error())
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

func (p *repoPicker) window(h repoPickerViewHost, count int) (start, end int) {
	_, height := h.size()
	visible := max(3, height-repoPickChrome)
	if count <= visible {
		return 0, count
	}
	start = p.cursor - visible/2
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

func (p *repoPicker) row(h repoPickerViewHost, row pickRow, selected bool) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle()
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		nameStyle = nameStyle.Foreground(colorAccent).Bold(true)
	}
	inner := h.cardWidth() - 2*cardPaddingX
	name := truncateTail(escapeControlsInline(row.label), inner-lipgloss.Width(marker))
	budget := inner - lipgloss.Width(marker) - lipgloss.Width(name) - 2
	dir := ""
	if budget > 1 {
		dir = subtleStyle.Render("  " + truncateTail(escapeControlsInline(filepath.Dir(row.root)), budget))
	}
	return marker + nameStyle.Render(name) + dir
}

func (p *repoPicker) view(h repoPickerViewHost) string {
	rows := p.filteredRows()
	var body strings.Builder
	body.WriteString(mutedStyle.Render("filter: ") + p.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no match"))
	}
	start, end := p.window(h, len(rows))
	for i := start; i < end; i++ {
		body.WriteString(p.row(h, rows[i], i == p.cursor) + "\n")
	}
	if hidden := len(rows) - (end - start); hidden > 0 {
		body.WriteString(subtleStyle.Render(fmt.Sprintf("+%d more", hidden)) + "\n")
	}
	return h.card(p.title, strings.TrimRight(body.String(), "\n"), [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵", "select"}, {"esc", "cancel"}})
}
