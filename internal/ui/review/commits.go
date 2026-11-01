package review

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// commitPillSubject caps the subject in the header's commit pill, which
	// shares the line with the scope, repo and target pills.
	commitPillSubject = 40
	// maxRailCommits caps the rail's commit rows so the file list keeps
	// most of the height.
	maxRailCommits = 5
)

// commitScopePrefix keys review state taken in the commit scope by the
// commit it was taken on, since the same path renders differently in each.
const commitScopePrefix = "commit@"

// CommitPin is the commit the commit scope shows; an empty SHA follows HEAD.
// Index is where the pinned commit stood in the list, the place to fall back
// to once a rewrite leaves nothing to match it by. Unreviewed asks the next
// load for the oldest commit not fully reviewed, falling back to HEAD.
type CommitPin struct {
	SHA        string
	Index      int
	Unreviewed bool
}

// CommitLoad is what a load takes to pick the commit scope's commit: the
// pin, the commits review state is keyed by so a rewrite can carry it, the
// HEAD that state was last carried at, and the reviewed marks and filter
// that decide which commits are done. Resume holds the pins a reopened
// review starts from, keyed by session and repo root.
type CommitLoad struct {
	Pin      CommitPin
	Keyed    []string
	Head     string
	Marks    map[string]uint64
	CodeOnly bool
	Resume   map[string]CommitPin
}

// RewriteCandidates lists the commits whose rewrites a load must look up:
// those review state was taken on, in memory or in saved, and the pin.
func (load CommitLoad) RewriteCandidates(saved *SavedState) []string {
	keyed := slices.Clone(load.Keyed)
	if saved != nil {
		keyed = append(keyed, keyedCommits(*saved)...)
	}
	if load.Pin.SHA != "" {
		keyed = append(keyed, load.Pin.SHA)
	}
	slices.Sort(keyed)
	return slices.Compact(keyed)
}

// ChooseCommit picks the commit a commit scope load shows from commits,
// newest first: the oldest not fully reviewed, the pinned commit, its
// replacement after a rewrite, or the commit now at its place in the list.
// "" follows HEAD. notice tells the user when the pinned commit moved.
func ChooseCommit(commits []git.Commit, rewrites map[string]string, load CommitLoad, saved *SavedState) (sha string, pinned bool, notice string) {
	if len(commits) == 0 {
		return "", false, ""
	}
	if load.Pin.Unreviewed {
		marks := maps.Clone(load.Marks)
		if marks == nil {
			marks = make(map[string]uint64)
		}
		if saved != nil {
			maps.Copy(marks, saved.Reviewed)
		}
		marks = carriedMarks(marks, rewrites)
		for i := len(commits) - 1; i > 0; i-- {
			if !commitReviewed(commits[i], marks, load.CodeOnly) {
				return commits[i].SHA, true, ""
			}
		}
		return "", false, ""
	}
	sha = load.Pin.SHA
	if sha == "" {
		return "", false, ""
	}
	if commitIndex(commits, sha) >= 0 {
		return sha, true, ""
	}
	if moved, ok := rewrites[sha]; ok {
		return moved, true, fmt.Sprintf("commit %s was rewritten as %s", shortSHA(sha), shortSHA(moved))
	}
	at := commits[min(load.Pin.Index, len(commits)-1)]
	return at.SHA, true, fmt.Sprintf("commit %s is no longer on the branch, showing %s", shortSHA(sha), at.Short)
}

// commitReviewed reports whether every path c changes carries a reviewed
// mark taken on c. The code-only filter leaves out the paths it hides, and a
// commit changing nothing has nothing left to review.
func commitReviewed(c git.Commit, marks map[string]uint64, codeOnly bool) bool {
	for _, path := range c.Files {
		if codeOnly && IsNonCodePath(path) {
			continue
		}
		if marks[commitScopePrefix+c.SHA+"\x00"+path] == 0 {
			return false
		}
	}
	return true
}

func (m Model) CommitReviewed(c git.Commit) bool { return m.commitIsReviewed(c) }

func (m Model) commitIsReviewed(c git.Commit) bool {
	return commitReviewed(c, m.reviewed[m.reviewKey()], m.codeOnly)
}

// carriedScope is scope with the commit it names replaced by the one a
// rewrite moved it to.
func carriedScope(scope string, moved map[string]string) (string, bool) {
	sha, ok := strings.CutPrefix(scope, commitScopePrefix)
	if to, found := moved[sha]; ok && found {
		return commitScopePrefix + to, true
	}
	return scope, false
}

// carriedMarks rekeys marks onto the commits a rewrite moved them to. A mark
// already on the replacement wins over one carried onto it.
func carriedMarks(marks map[string]uint64, moved map[string]string) map[string]uint64 {
	carried := make(map[string]uint64, len(marks))
	for markKey, hash := range marks {
		scope, path, _ := strings.Cut(markKey, "\x00")
		if to, ok := carriedScope(scope, moved); ok {
			if _, taken := marks[to+"\x00"+path]; !taken {
				carried[to+"\x00"+path] = hash
			}
			continue
		}
		carried[markKey] = hash
	}
	return carried
}

// keyedCommits lists the commits review state was taken on.
func keyedCommits(state SavedState) []string {
	var shas []string
	add := func(scope string) {
		if sha, ok := strings.CutPrefix(scope, commitScopePrefix); ok && sha != "" {
			shas = append(shas, sha)
		}
	}
	for markKey := range state.Reviewed {
		scope, _, _ := strings.Cut(markKey, "\x00")
		add(scope)
	}
	for _, note := range state.Comments {
		add(note.Scope)
	}
	add(state.Round.Scope)
	return shas
}

func commitIndex(commits []git.Commit, sha string) int {
	return slices.IndexFunc(commits, func(c git.Commit) bool { return c.SHA == sha })
}

// shortSHA abbreviates a commit for text an agent reads, long enough to stay
// unambiguous where git's own short form is not at hand.
func shortSHA(sha string) string {
	return sha[:min(len(sha), 12)]
}

func (m Model) commitLoad() CommitLoad {
	state := m.savedState(m.reviewKey())
	return CommitLoad{
		Pin:      m.commitPin,
		Keyed:    keyedCommits(state),
		Head:     m.rewriteHead,
		Marks:    state.Reviewed,
		CodeOnly: m.codeOnly,
	}
}

// carryRewrites moves review state taken on commits a rebase or amend
// replaced onto their replacements. The reload's content hashes then judge
// whether each mark and comment still holds there.
func (m *Model) carryRewrites(moved map[string]string) bool {
	if len(moved) == 0 {
		return false
	}
	key := m.reviewKey()
	changed := false
	if marks := m.reviewed[key]; marks != nil {
		carried := carriedMarks(marks, moved)
		changed = !maps.Equal(carried, marks)
		m.reviewed[key] = carried
	}
	notes := m.annotations[key]
	for i := range notes {
		if to, ok := carriedScope(notes[i].scope, moved); ok {
			notes[i].scope = to
			changed = true
		}
	}
	if round, ok := m.rounds[key]; ok {
		if to, carried := carriedScope(round.Scope, moved); carried {
			round.Scope = to
			m.rounds[key] = round
			changed = true
		}
	}
	return changed
}

// settleCommit records the commit a commit scope load resolved and carries
// review state across the rewrites it found, reporting whether state moved.
func (m *Model) settleCommit(result LoadResult, accepted *ApplyResult) bool {
	m.rewriteHead = result.Head
	m.commitPin = CommitPin{}
	if result.Pinned {
		m.commitPin = CommitPin{SHA: m.set.Commit, Index: max(commitIndex(result.Commits, m.set.Commit), 0)}
	}
	if m.lastPins == nil {
		m.lastPins = make(map[string]CommitPin)
	}
	m.lastPins[m.reviewKey()] = m.commitPin
	if result.CommitNotice != "" {
		m.notice = result.CommitNotice
	}
	if result.RewriteErr != nil {
		accepted.Error = "following rewritten commits: " + result.RewriteErr.Error()
	}
	return m.carryRewrites(result.Rewrites)
}

// resetCommitScope forgets the commit scope's commit and list, for a review
// moving to another session or repo.
func (m *Model) resetCommitScope(pin CommitPin) {
	m.commitPin = pin
	m.commits = nil
	m.commitTarget = ""
	m.rewriteHead = ""
}

// reloadScope loads the diff afresh after the scope or the commit in view
// changed, dropping the place kept in the old one.
func (m *Model) reloadScope() LoadRequest {
	m.gen++
	m.loading = true
	m.errText = ""
	m.markAll = false
	// The repo stays: the reload reads the same one, and the header's repo,
	// target and commit pills hold still instead of blinking out with the
	// files.
	m.set = diff.Set{Repo: m.set.Repo, BaseOverride: m.set.BaseOverride}
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	return LoadRequest{
		Target: m.target, Scope: m.scope, Generation: m.gen,
		RepoWanted: m.repoSel, RepoRoot: m.repoSel,
		RepoRoots: append([]string(nil), m.repoRoots...),
		Resolve:   m.repoSel == "" || len(m.repoRoots) == 0,
		Restored:  maps.Clone(m.stateLoaded),
		Commit:    m.commitLoad(),
	}
}

// StepCommit moves the commit scope delta commits older, or newer when
// negative. Reaching the newest follows HEAD again.
func (m *Model) StepCommit(delta int) (LoadRequest, bool) {
	if !m.active || m.scope != git.ScopeCommit || len(m.commits) == 0 {
		return LoadRequest{}, false
	}
	at := max(commitIndex(m.commits, m.viewedCommit()), 0)
	next := min(max(at+delta, 0), len(m.commits)-1)
	if next == at {
		m.notice = m.commitEndNotice(delta < 0)
		return LoadRequest{}, false
	}
	return m.showCommit(next)
}

// commitEndNotice answers a step past either end of the commit list, saying
// what is left to review there.
func (m Model) commitEndNotice(newest bool) string {
	left := 0
	for _, c := range m.commits {
		if !m.commitIsReviewed(c) {
			left++
		}
	}
	total := len(m.commits)
	switch {
	case left == 0:
		return fmt.Sprintf("all %d %s reviewed", total, commitNoun(total))
	case newest:
		return fmt.Sprintf("HEAD is the newest commit, %d of %d not fully reviewed", left, total)
	default:
		return fmt.Sprintf("oldest commit, %d of %d not fully reviewed", left, total)
	}
}

func commitNoun(count int) string {
	if count == 1 {
		return "commit"
	}
	return "commits"
}

// ShowCommit pins the commit scope to sha, or follows HEAD for the newest.
func (m *Model) ShowCommit(sha string) (LoadRequest, bool) {
	if !m.active || m.scope != git.ScopeCommit {
		return LoadRequest{}, false
	}
	return m.showCommit(commitIndex(m.commits, sha))
}

// ClickCommitMark marks every file of the commit whose glyph row line of the
// commit rail painted reviewed, the way MarkAll does, opening it first when
// another commit is in view.
func (m *Model) ClickCommitMark(line int) Requests {
	index, ok := m.commitMarks.At(line)
	if !ok || m.paused() {
		return Requests{}
	}
	if index == commitIndex(m.commits, m.viewedCommit()) {
		return m.MarkAll()
	}
	request, ok := m.showCommit(index)
	if !ok {
		return Requests{}
	}
	m.markLoad = request.Generation
	return Requests{Load: &request, StartupTick: true}
}

func (m *Model) showCommit(index int) (LoadRequest, bool) {
	if m.target.ID == "" || index < 0 || index >= len(m.commits) {
		return LoadRequest{}, false
	}
	m.commitPin = CommitPin{}
	if index > 0 {
		m.commitPin = CommitPin{SHA: m.commits[index].SHA, Index: index}
	}
	return m.reloadScope(), true
}

// viewedCommit is the commit in view, or while a step or pick loads, the one
// it is bringing in, so the counter, rail and pill move straight to it.
func (m Model) viewedCommit() string {
	switch {
	case m.set.Commit != "":
		return m.set.Commit
	case m.commitPin.SHA != "":
		return m.commitPin.SHA
	case !m.commitPin.Unreviewed && len(m.commits) > 0:
		return m.commits[0].SHA
	}
	return ""
}

// CommitPosition numbers the commit in view from the oldest, 1, to HEAD,
// total; at is 0 when the list does not hold it.
func (m Model) CommitPosition() (at, total int) {
	total = len(m.commits)
	if i := commitIndex(m.commits, m.viewedCommit()); i >= 0 {
		at = total - i
	}
	return at, total
}

// shownCommit is the commit list's entry for the commit in view.
func (m Model) shownCommit() (git.Commit, bool) {
	at := commitIndex(m.commits, m.viewedCommit())
	if at < 0 {
		return git.Commit{}, false
	}
	return m.commits[at], true
}

// scopeKey names the rendering review state is taken against: a scope, or
// in the commit scope the commit in view.
func (m Model) scopeKey() string {
	if m.scope == git.ScopeCommit {
		return commitScopePrefix + m.set.Commit
	}
	return m.scope.String()
}

// inScope reports whether a comment's scope is the rendering in view. One
// with no scope predates scoped comments and matches any scope but a commit,
// which no comment reached before commits had a scope of their own.
func (m Model) inScope(scope string) bool {
	if scope == "" {
		return m.scope != git.ScopeCommit
	}
	return scope == m.scopeKey()
}

// noteShown reports whether a comment belongs on the rendering in view. One
// taken on a commit shows only there, where its line numbers mean something.
func (m Model) noteShown(note *annotation) bool {
	return !strings.HasPrefix(note.scope, commitScopePrefix) || note.scope == m.scopeKey()
}

// MarkAll marks every file the list shows reviewed, or clears them all when
// each already is. A mark hashes the file's rendering, so files not loaded
// yet load first and the marks land once the last one arrives.
func (m *Model) MarkAll() Requests {
	shown, marked := 0, 0
	for i := range m.set.Files {
		fd := &m.set.Files[i]
		if m.fileHidden(fd) {
			continue
		}
		shown++
		if m.FileReviewed(fd.File.Path) {
			marked++
		}
	}
	if shown == 0 {
		return Requests{}
	}
	if marked == shown {
		marks := m.reviewed[m.reviewKey()]
		for i := range m.set.Files {
			if fd := &m.set.Files[i]; !m.fileHidden(fd) {
				delete(marks, m.markKey(fd.File.Path))
			}
		}
		m.notice = fmt.Sprintf("cleared %d reviewed %s", shown, fileNoun(shown))
		return Requests{Save: m.saveRequest()}
	}
	m.markAll = true
	if done, ok := m.finishMarkAll(); ok {
		return done
	}
	var requests Requests
	for i := range m.set.Files {
		if fd := &m.set.Files[i]; !fd.Loaded() && !m.fileHidden(fd) {
			if request, ok := m.requestFile(i); ok {
				requests.Files = append(requests.Files, request)
			}
		}
	}
	m.notice = fmt.Sprintf("loading %d %s to mark reviewed", len(requests.Files), fileNoun(len(requests.Files)))
	return requests
}

// finishMarkAll lands a pending MarkAll once every shown file has loaded.
func (m *Model) finishMarkAll() (Requests, bool) {
	if !m.markAll {
		return Requests{}, false
	}
	for i := range m.set.Files {
		if fd := &m.set.Files[i]; !fd.Loaded() && !m.fileHidden(fd) {
			return Requests{}, false
		}
	}
	m.markAll = false
	key := m.reviewKey()
	marks := m.reviewed[key]
	if marks == nil {
		marks = make(map[string]uint64)
		m.reviewed[key] = marks
	}
	count := 0
	for i := range m.set.Files {
		if fd := &m.set.Files[i]; !m.fileHidden(fd) {
			marks[m.markKey(fd.File.Path)] = ContentHash(fd)
			count++
		}
	}
	m.notice = fmt.Sprintf("marked %d %s reviewed", count, fileNoun(count))
	return Requests{Save: m.saveRequest()}, true
}

func fileNoun(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

// CommitPickerRows lists the commit scope's commits, newest first, for the
// commit picker, with the SHA of the one in view.
func (m Model) CommitPickerRows() (rows []PickerRow, current string, err string) {
	if m.scope != git.ScopeCommit {
		return nil, "", "s to the commit scope first"
	}
	if len(m.commits) == 0 {
		return nil, "", "no commits to review"
	}
	rows = make([]PickerRow, len(m.commits))
	for i, c := range m.commits {
		glyph := "○ "
		if m.commitIsReviewed(c) {
			glyph = "✔ "
		}
		label := glyph + c.Short + " " + c.Subject
		if i == 0 {
			label += " (HEAD)"
		}
		rows[i] = PickerRow{Label: label, Root: c.SHA}
	}
	return rows, m.viewedCommit(), ""
}

// CommitRail lists the commit scope's commits oldest first, the one in view
// marked, each with whether every file in it is reviewed. It shows a window
// of at most maxRailCommits rows around the commit in view, and "" outside
// the commit scope.
func (m *Model) CommitRail(ctx RenderContext, width, height int) string {
	s := ctx.Styles
	commits := m.commits
	m.commitRows.Reset()
	m.commitMarks.Reset()
	if m.scope != git.ScopeCommit || len(commits) == 0 {
		return ""
	}
	current := commitIndex(commits, m.viewedCommit())
	cursor := 0
	if current >= 0 {
		cursor = len(commits) - 1 - current
	}
	start, end := scrollWindow(len(commits), cursor, max(min(height, maxRailCommits), 3))
	var b strings.Builder
	if start > 0 {
		b.WriteString(s.Subtle.Render("  "+presentation.OverflowLabel(ctx.GlyphMode, start, true)) + "\n")
		m.commitRows.Show(len(commits)-start, 1)
		m.commitMarks.Skip(1)
	}
	for pos := start; pos < end; pos++ {
		i := len(commits) - 1 - pos
		m.commitRows.Show(i, 1)
		m.commitMarks.Show(i, 1)
		c := commits[i]
		glyph := s.Subtle.Render("○")
		if m.commitIsReviewed(c) {
			glyph = lipgloss.NewStyle().Foreground(s.Finished).Render("✔")
		}
		bar, subject := " ", s.Value
		if i == current {
			bar = lipgloss.NewStyle().Foreground(s.Accent).Render("▎")
			subject = lipgloss.NewStyle().Foreground(s.Accent).Bold(true)
		}
		tag := ""
		if i == 0 {
			tag = s.Subtle.Render("HEAD")
		}
		left := bar + glyph + " " + s.Muted.Render(c.Short) + " "
		budget := max(width-ansi.StringWidth(left)-ansi.StringWidth(tag)-1, 4)
		row := left + subject.Render(presentation.TruncateTail(presentation.EscapeControlsInline(c.Subject), budget))
		if tag != "" {
			row += strings.Repeat(" ", max(width-ansi.StringWidth(row)-ansi.StringWidth(tag), 1)) + tag
		}
		b.WriteString(row + "\n")
	}
	if end < len(commits) {
		b.WriteString(s.Subtle.Render("  " + presentation.OverflowLabel(ctx.GlyphMode, len(commits)-end, false)))
		m.commitRows.Show(len(commits)-1-end, 1)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ClickCommit is the commit row line of the commit rail last painted, when
// a click there should show it.
func (m *Model) ClickCommit(line int) (string, bool) {
	i, ok := m.commitRows.At(line)
	if !ok || m.paused() || i == commitIndex(m.commits, m.viewedCommit()) {
		return "", false
	}
	return m.commits[i].SHA, true
}
