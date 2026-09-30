package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"unicode"
)

func (m *Model) openAnnotate() {
	fd := m.currentFileDiff()
	if fd == nil || m.diffFileHidden(fd) {
		return
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) || fd.Lines[lineIdx].Kind == diff.Gap {
		return
	}
	input := textarea.New()
	input.CharLimit = 500
	input.Placeholder = "comment for the agent"
	input.ShowLineNumbers = false
	input.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "¶ "
		}
		return ""
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.SetHeight(1)
	if existing := m.annotationAt(fd.File.Path, fd.Lines[lineIdx]); existing != nil {
		input.SetValue(existing.text)
	}
	input.Focus()
	m.diff.annInput = input
	m.diff.annotating = true
}

func annotationLine(line diff.Line) (num int, deleted bool) {
	if line.Kind == diff.Del {
		return line.OldNum, true
	}
	return line.NewNum, false
}

func (m *Model) annotationAt(path string, line diff.Line) *annotation {
	num, deleted := annotationLine(line)
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			return &notes[i]
		}
	}
	return nil
}

func (m *Model) annotationsAt(path string, line diff.Line) []*annotation {
	num, deleted := annotationLine(line)
	notes := m.diff.annotations[m.reviewKey()]
	var matched []*annotation
	for i := range notes {
		if notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			matched = append(matched, &notes[i])
		}
	}
	return matched
}

func (m *Model) draftAnnotationCount() int {
	count := 0
	for _, note := range m.diff.annotations[m.reviewKey()] {
		if note.round == 0 {
			count++
		}
	}
	return count
}

// annotationOccupies reports whether a note other than self already anchors
// on the given file line, so re-anchoring never stacks two comments there.
func (m *Model) annotationOccupies(file string, line int, deleted bool, self int) bool {
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if i != self && notes[i].file == file && notes[i].line == line && notes[i].deleted == deleted {
			return true
		}
	}
	return false
}

func (m *Model) handleAnnotateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.diff.annotating = false
		return m, nil
	case "enter":
		return m, m.saveAnnotation()
	}
	var cmd tea.Cmd
	m.diff.annInput, cmd = m.diff.annInput.Update(msg)
	return m, cmd
}

func (m *Model) saveAnnotation() tea.Cmd {
	m.diff.annotating = false
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return nil
	}
	text := strings.TrimSpace(withoutControlBytes(m.diff.annInput.Value()))
	line := fd.Lines[lineIdx]
	num, deleted := annotationLine(line)
	if existing := m.annotationAt(fd.File.Path, line); existing != nil {
		if text == "" {
			return m.discardOrToggleAnnotation()
		}
		existing.text = text
		existing.hash = contentHash(fd)
		existing.scope = m.diff.scope.String()
		return m.saveReviewStateCmd()
	}
	if text == "" {
		return nil
	}
	m.diff.annotations[m.reviewKey()] = append(m.diff.annotations[m.reviewKey()], annotation{
		id:      newReviewCommentID(),
		file:    fd.File.Path,
		line:    num,
		deleted: deleted,
		excerpt: excerptOf(line.Text),
		text:    text,
		hash:    contentHash(fd),
		scope:   m.diff.scope.String(),
	})
	return m.saveReviewStateCmd()
}

func (m *Model) discardOrToggleAnnotation() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return nil
	}
	num, deleted := annotationLine(fd.Lines[lineIdx])
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == fd.File.Path && notes[i].line == num && notes[i].deleted == deleted {
			m.diff.annotations[m.reviewKey()] = append(notes[:i], notes[i+1:]...)
			return m.saveReviewStateCmd()
		}
	}
	latestOpen := -1
	latestHandled := -1
	for i := range notes {
		if notes[i].round == 0 || notes[i].file != fd.File.Path || notes[i].line != num || notes[i].deleted != deleted {
			continue
		}
		if notes[i].handled {
			if latestHandled < 0 || notes[i].round >= notes[latestHandled].round {
				latestHandled = i
			}
		} else if latestOpen < 0 || notes[i].round >= notes[latestOpen].round {
			latestOpen = i
		}
	}
	target := latestOpen
	if target < 0 {
		target = latestHandled
	}
	if target >= 0 {
		if m.services.store == nil {
			m.errBar.text = "review state is unavailable"
			return nil
		}
		previous := notes[target].handled
		handled := !notes[target].handled
		m.diff.reviewStatusGen++
		notes[target].handled = handled
		m.diff.annotations[m.reviewKey()] = notes
		return m.reviewCommentHandledCmd(notes[target].id, handled, previous)
	}
	return nil
}

// Newlines would submit the prompt before every comment reaches the pane.
func (m *Model) sendAnnotations() (tea.Model, tea.Cmd) {
	if m.diff.reviewSendPending {
		m.errBar.text = "review round is already being sent"
		return m, nil
	}
	if m.services.store == nil {
		m.errBar.text = "review state is unavailable"
		return m, nil
	}
	sess, ok := m.diffSession()
	if !ok {
		m.errBar.text = "session is gone"
		return m, nil
	}
	if m.isShell(sess.Tool) {
		m.errBar.text = shellPromptHint(sess.Name)
		return m, nil
	}
	key := m.reviewKey()
	previousState := m.reviewStateSnapshot(key)
	notes := append([]annotation(nil), m.diff.annotations[key]...)
	round := m.diff.rounds[key]
	previousRound := round
	nextRound := round.Number + 1
	var parts []string
	draftIndexes := make([]int, 0, len(notes))
	for i, note := range notes {
		if note.round != 0 {
			continue
		}
		draftIndexes = append(draftIndexes, i)
		notes[i].point = len(parts) + 1
		location := fmt.Sprintf("%s:%d", note.file, note.line)
		body := strings.ReplaceAll(note.text, "\n", " / ")
		if note.deleted {
			parts = append(parts, fmt.Sprintf("(%d) [comment %s] %s (deleted line): %s", len(parts)+1, notes[i].id, location, body))
		} else {
			parts = append(parts, fmt.Sprintf("(%d) [comment %s] %s (code: `%s`): %s", len(parts)+1, notes[i].id, location, note.excerpt, body))
		}
	}
	if len(parts) == 0 {
		m.errBar.text = "no comments to send - press c on a line first"
		return m, nil
	}
	prompt := fmt.Sprintf(
		"Code review of %s. Address each numbered point, then mark its comment handled with the review_comment tool (or `agent-manager review-comment <comment-id>`), and summarize what you changed per point: %s",
		scopePhrase(m.diff.scope), strings.Join(parts, "; "))
	round.Number = nextRound
	round.Scope = m.diff.scope.String()
	round.Fingerprint = m.diff.fingerprint
	m.diff.rounds[key] = round
	for _, i := range draftIndexes {
		notes[i].round = round.Number
		notes[i].scope = round.Scope
		notes[i].handled = false
		notes[i].outdated = false
	}
	m.diff.annotations[key] = notes
	count := len(draftIndexes)
	commentIDs := make([]string, 0, len(draftIndexes))
	for _, i := range draftIndexes {
		commentIDs = append(commentIDs, notes[i].id)
	}
	m.diff.reviewSendPending = true
	m.diff.notice = fmt.Sprintf("sending review round %d to %s", round.Number, sess.Name)
	return m, m.reviewSendCmd(reviewSendRequest{
		sess: sess, prompt: prompt,
		state: m.reviewStateSnapshot(key), previousState: previousState,
		commentIDs: commentIDs, previousRound: previousRound,
		round: round.Number, count: count,
	})
}

// excerptOf caps a code excerpt at 60 runes, never splitting a rune, so
// multibyte lines stay valid UTF-8 in the prompt sent to the agent.
// A comment and its excerpt are painted into the pane and pasted into the
// agent's prompt, so a control byte from the file under review or from a
// paste would drive the terminal instead of reading as text.
func withoutControlBytes(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, text)
}

func excerptOf(text string) string {
	excerpt := strings.TrimSpace(withoutControlBytes(text))
	if runes := []rune(excerpt); len(runes) > 60 {
		return string(runes[:60])
	}
	return excerpt
}

func commentNoun(count int) string {
	if count == 1 {
		return "comment"
	}
	return "comments"
}

func newReviewCommentID() string {
	return newID() + newID()
}

func scopePhrase(scope git.Scope) string {
	switch scope {
	case git.ScopeBranch:
		return "your branch changes vs target"
	case git.ScopeLastCommit:
		return "your last commit"
	case git.ScopeStaged:
		return "your staged changes"
	default:
		return "your uncommitted changes"
	}
}
