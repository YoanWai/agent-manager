package review

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) OpenAnnotation() bool {
	fd := m.currentFile()
	if fd == nil || m.fileHidden(fd) {
		return false
	}
	lineIdx := m.CursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) || fd.Lines[lineIdx].Kind == diff.Gap {
		return false
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
	m.annotating = input
	m.annotationOpen = true
	return true
}

func (m *Model) PrepareAnnotation(width, height int) {
	if !m.annotationOpen {
		return
	}
	m.annotating.SetWidth(width)
	m.annotating.SetHeight(height)
}

func (m Model) AnnotationValue() string { return m.annotating.Value() }

// AnnotationView renders the owned editor without exposing its mutable Bubbles
// model. cursorMarker follows the root IME marker convention.
func (m Model) AnnotationView(cursorMarker string) string {
	input := m.annotating
	if !input.Focused() {
		return input.View()
	}
	marked := input
	marked.Cursor.Blink = false
	style := marked.Cursor.Style
	transform := style.GetTransform()
	marked.Cursor.Style = style.Transform(func(value string) string {
		if transform != nil {
			value = transform(value)
		}
		return cursorMarker + value
	})
	markedView := marked.View()
	if !input.Cursor.Blink {
		return markedView
	}
	return insertMarkerAtCursor(input.View(), markedView, cursorMarker)
}

func insertMarkerAtCursor(normal, marked, marker string) string {
	index := strings.Index(marked, marker)
	if index < 0 {
		return normal
	}
	prefix := marked[:index]
	row := strings.Count(prefix, "\n")
	if newline := strings.LastIndexByte(prefix, '\n'); newline >= 0 {
		prefix = prefix[newline+1:]
	}
	column := ansi.StringWidth(prefix)
	lines := strings.Split(normal, "\n")
	if row >= len(lines) {
		return normal
	}
	line := lines[row]
	cell, state := 0, ansi.NormalState
	for offset := 0; offset < len(line); {
		_, width, size, nextState := ansi.GraphemeWidth.DecodeSequenceInString(line[offset:], state, nil)
		if size <= 0 {
			break
		}
		if width > 0 && cell+width > column {
			lines[row] = line[:offset] + marker + line[offset:]
			return strings.Join(lines, "\n")
		}
		cell += width
		offset += size
		state = nextState
	}
	if cell == column {
		lines[row] += marker
		return strings.Join(lines, "\n")
	}
	return normal
}

func (m *Model) AnnotationKey(msg tea.KeyMsg) KeyResult {
	switch msg.String() {
	case "ctrl+c":
		return KeyResult{Navigation: NavigationQuit, Consumed: true}
	case "esc":
		m.annotationOpen = false
		return KeyResult{Consumed: true}
	case "enter":
		return KeyResult{Requests: m.saveAnnotation(), Consumed: true}
	}
	var cmd tea.Cmd
	m.annotating, cmd = m.annotating.Update(msg)
	return KeyResult{Requests: Requests{WidgetCmd: cmd}, Consumed: true}
}

func (m *Model) saveAnnotation() Requests {
	m.annotationOpen = false
	fd := m.currentFile()
	if fd == nil {
		return Requests{}
	}
	lineIdx := m.CursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return Requests{}
	}
	text := strings.TrimSpace(stripControls(m.annotating.Value()))
	line := fd.Lines[lineIdx]
	num, deleted := annotationLine(line)
	if existing := m.annotationAt(fd.File.Path, line); existing != nil {
		if text == "" {
			requests, _ := m.DiscardOrToggle(true)
			return requests
		}
		existing.text = text
		existing.hash = ContentHash(fd)
		existing.scope = m.scope.String()
		return Requests{Save: m.saveRequest()}
	}
	if text == "" {
		return Requests{}
	}
	key := m.reviewKey()
	m.annotations[key] = append(m.annotations[key], annotation{
		id: newCommentID(), file: fd.File.Path, line: num, deleted: deleted,
		excerpt: excerptOf(line.Text), text: text, hash: ContentHash(fd), scope: m.scope.String(),
	})
	return Requests{Save: m.saveRequest()}
}

func (m Model) annotationAt(path string, line diff.Line) *annotation {
	num, deleted := annotationLine(line)
	notes := m.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			return &notes[i]
		}
	}
	return nil
}

func (m Model) AnnotationsAt(path string, line diff.Line) []Comment {
	num, deleted := annotationLine(line)
	var out []Comment
	for _, note := range m.annotations[m.reviewKey()] {
		if note.file == path && note.line == num && note.deleted == deleted {
			out = append(out, commentValue(note))
		}
	}
	return out
}

func (m Model) Annotations() []Comment {
	notes := m.annotations[m.reviewKey()]
	out := make([]Comment, 0, len(notes))
	for _, note := range notes {
		out = append(out, commentValue(note))
	}
	return out
}

func commentValue(note annotation) Comment {
	return Comment{
		ID: note.id, File: note.file, Line: note.line, Deleted: note.deleted,
		Excerpt: note.excerpt, Text: note.text, ContentHash: note.hash,
		Round: note.round, Scope: note.scope, Point: note.point,
		Resolved: note.handled, Outdated: note.outdated,
	}
}

func (m Model) DraftCount() int {
	count := 0
	for _, note := range m.annotations[m.reviewKey()] {
		if note.round == 0 {
			count++
		}
	}
	return count
}

func (m *Model) DiscardOrToggle(persistenceAvailable bool) (Requests, string) {
	fd := m.currentFile()
	if fd == nil {
		return Requests{}, ""
	}
	lineIdx := m.CursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return Requests{}, ""
	}
	num, deleted := annotationLine(fd.Lines[lineIdx])
	key := m.reviewKey()
	notes := m.annotations[key]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == fd.File.Path && notes[i].line == num && notes[i].deleted == deleted {
			m.annotations[key] = append(notes[:i], notes[i+1:]...)
			return Requests{Save: m.saveRequest()}, ""
		}
	}
	latestOpen, latestHandled := -1, -1
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
	if target < 0 {
		return Requests{}, ""
	}
	if !persistenceAvailable {
		return Requests{}, "review state is unavailable"
	}
	previous := notes[target].handled
	handled := !previous
	m.statusGen++
	notes[target].handled = handled
	m.annotations[key] = notes
	return Requests{Handle: &HandleCommentRequest{
		TargetID: m.target.ID, RepoRoot: m.repoSel, CommentID: notes[target].id,
		Handled: handled, Previous: previous,
	}}, ""
}

func (m *Model) ApplyHandle(result HandleCommentResult) ApplyResult {
	accepted := ApplyResult{Accepted: true}
	if result.Err == nil && result.Found {
		return accepted
	}
	key := result.TargetID + "\x00" + result.RepoRoot
	notes := m.annotations[key]
	for i := range notes {
		if notes[i].id == result.CommentID && notes[i].handled == result.Handled {
			notes[i].handled = result.Previous
		}
	}
	m.annotations[key] = notes
	if result.TargetID != m.target.ID || result.RepoRoot != m.repoSel {
		return accepted
	}
	if result.Err != nil {
		accepted.Error = "updating review comment: " + result.Err.Error()
	} else {
		accepted.Error = "review comment no longer exists"
	}
	return accepted
}

func (m *Model) BeginSend() ApplyResult {
	if m.sendPending {
		return ApplyResult{Accepted: true, Error: "review round is already being sent"}
	}
	key := m.reviewKey()
	previousState := m.savedState(key)
	notes := append([]annotation(nil), m.annotations[key]...)
	round := m.rounds[key]
	previousRound := round
	nextRound := round.Number + 1
	var parts []string
	var indexes []int
	for i, note := range notes {
		if note.round != 0 {
			continue
		}
		indexes = append(indexes, i)
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
		return ApplyResult{Accepted: true, Error: "no comments to send - press c on a line first"}
	}
	prompt := fmt.Sprintf("Code review of %s. Address each numbered point, then mark its comment handled with the review_comment tool (or `agent-manager review-comment <comment-id>`), and summarize what you changed per point: %s", scopePhrase(m.scope), strings.Join(parts, "; "))
	round.Number = nextRound
	round.Scope = m.scope.String()
	round.Fingerprint = m.fingerprint
	m.rounds[key] = round
	ids := make([]string, 0, len(indexes))
	for _, i := range indexes {
		notes[i].round = round.Number
		notes[i].scope = round.Scope
		notes[i].handled = false
		notes[i].outdated = false
		ids = append(ids, notes[i].id)
	}
	m.annotations[key] = notes
	m.sendPending = true
	m.notice = fmt.Sprintf("sending review round %d to %s", round.Number, m.target.Name)
	return ApplyResult{Accepted: true, Requests: Requests{Send: &SendRequest{
		Target: m.target, RepoRoot: m.repoSel, Prompt: prompt,
		State: m.savedState(key), PreviousState: previousState,
		CommentIDs: ids, PreviousRound: previousRound,
		Round: round.Number, Count: len(indexes),
	}}, Notice: m.notice}
}

func (m *Model) ApplySend(result SendResult) ApplyResult {
	m.sendPending = false
	key := result.TargetID + "\x00" + result.RepoRoot
	if !result.Delivered {
		ids := make(map[string]bool, len(result.CommentIDs))
		for _, id := range result.CommentIDs {
			ids[id] = true
		}
		notes := m.annotations[key]
		for i := range notes {
			if ids[notes[i].id] && notes[i].round == result.Round {
				notes[i].round = 0
				notes[i].point = 0
			}
		}
		m.annotations[key] = notes
		if m.rounds[key].Number == result.Round {
			m.rounds[key] = result.PreviousRound
		}
	}
	accepted := ApplyResult{Accepted: true}
	if result.TargetID != m.target.ID || result.RepoRoot != m.repoSel {
		return accepted
	}
	if result.Err != nil {
		m.notice = ""
		accepted.Error = result.Err.Error()
		return accepted
	}
	m.notice = fmt.Sprintf("sent review round %d (%d %s) to %s", result.Round, result.Count, commentNoun(result.Count), result.TargetName)
	accepted.Notice = m.notice
	accepted.Refresh = true
	if result.AckErr != nil {
		accepted.Error = "comments sent, but clearing the alert ack failed: " + result.AckErr.Error()
	}
	return accepted
}

func (m *Model) SetSendConfirm(open bool) { m.sendConfirm = open }
func (m Model) Round() Round              { return m.rounds[m.reviewKey()] }

func newCommentID() string { return shortID() + shortID() }

func shortID() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405")))
	}
	return hex.EncodeToString(buf)
}

// NormalizeSavedState owns the backward-compatible comment identity and point
// migration. The root adapter persists the returned value when changed.
func NormalizeSavedState(state SavedState) (SavedState, bool) {
	highest := make(map[int]int)
	for _, note := range state.Comments {
		if note.Round > 0 && note.Point > highest[note.Round] {
			highest[note.Round] = note.Point
		}
	}
	changed := false
	for i := range state.Comments {
		if state.Comments[i].ID == "" {
			state.Comments[i].ID = newCommentID()
			changed = true
		}
		if state.Comments[i].Round > 0 && state.Comments[i].Point == 0 {
			highest[state.Comments[i].Round]++
			state.Comments[i].Point = highest[state.Comments[i].Round]
			changed = true
		}
	}
	return state, changed
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

func commentNoun(count int) string {
	if count == 1 {
		return "comment"
	}
	return "comments"
}

func CommentNoun(count int) string { return commentNoun(count) }
