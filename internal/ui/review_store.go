package ui

import (
	"errors"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

// reviewKey scopes a session's comments and reviewed marks to the repo under
// review, so switching repos never leaks marks between same-named files.
func (m *Model) reviewKey() string {
	return m.diff.sessID + "\x00" + m.diff.repoSel
}

// reviewedMarkKey scopes a reviewed mark to the diff scope it was taken in:
// the same file renders different lines - and so a different content hash -
// under each scope, so only its own scope can read or judge the mark.
func (m *Model) reviewedMarkKey(path string) string {
	return m.diff.scope.String() + "\x00" + path
}

func readReviewState(stor *store.Store, sessID, repoRoot string) (store.ReviewState, error) {
	state, err := stor.ReviewState(sessID, repoRoot)
	if err != nil {
		return store.ReviewState{}, err
	}
	// A round's numbered points are read before any are handed out, so a
	// point a comment already carries cannot be handed to another one.
	highest := map[int]int{}
	for _, note := range state.Comments {
		if note.Round > 0 && note.Point > highest[note.Round] {
			highest[note.Round] = note.Point
		}
	}
	migrated := false
	for i := range state.Comments {
		note := &state.Comments[i]
		if note.ID == "" {
			note.ID = newReviewCommentID()
			migrated = true
		}
		if note.Round > 0 && note.Point == 0 {
			highest[note.Round]++
			note.Point = highest[note.Round]
			migrated = true
		}
	}
	// Merge rather than set: this write lands outside the review write
	// chain, and merging keeps a status an agent set while the load ran.
	if migrated {
		if err := stor.MergeReviewState(sessID, repoRoot, state); err != nil {
			return store.ReviewState{}, err
		}
	}
	return state, nil
}

func (m *Model) restoreReviewState(state store.ReviewState) bool {
	key := m.reviewKey()
	if m.diff.sessID == "" || m.diff.repoSel == "" || m.diff.stateLoaded[key] {
		return false
	}
	marks := make(map[string]uint64, len(state.Reviewed))
	for markKey, hash := range state.Reviewed {
		// A mark saved before marks were scope-keyed carries a bare path; it
		// can never match a scoped lookup, so dropping it here lets the next
		// save clear it from the row.
		if hash != 0 && strings.Contains(markKey, "\x00") {
			marks[markKey] = hash
		}
	}
	notes := make([]annotation, 0, len(state.Comments))
	for i := range state.Comments {
		note := &state.Comments[i]
		notes = append(notes, annotation{
			id: note.ID, file: note.File, line: note.Line, deleted: note.Deleted,
			excerpt: withoutControlBytes(note.Excerpt), text: withoutControlBytes(note.Text), hash: note.ContentHash,
			round: note.Round, scope: note.Scope, point: note.Point, handled: note.Resolved, outdated: note.Outdated,
		})
	}
	m.diff.reviewed[key] = marks
	m.diff.annotations[key] = notes
	m.diff.rounds[key] = state.Round
	m.diff.stateLoaded[key] = true
	return true
}

func (m *Model) reviewStatusesCmd() tea.Cmd {
	if m.services.store == nil || !m.diff.active || m.diff.sessID == "" || m.diff.repoSel == "" {
		return nil
	}
	m.diff.reviewStatusGen++
	gen := m.diff.reviewStatusGen
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.services.store
	writesDone := m.diff.reviewWriteDone
	return func() tea.Msg {
		if writesDone != nil {
			<-writesDone
		}
		state, err := stor.ReviewState(sessID, repoRoot)
		if err != nil {
			return reviewStatusesLoadedMsg{sessID: sessID, repoRoot: repoRoot, gen: gen, err: err}
		}
		handled := make(map[string]bool, len(state.Comments))
		for _, comment := range state.Comments {
			if comment.ID != "" && comment.Round > 0 {
				handled[comment.ID] = comment.Resolved
			}
		}
		return reviewStatusesLoadedMsg{sessID: sessID, repoRoot: repoRoot, gen: gen, handled: handled}
	}
}

func (m *Model) handleReviewStatusesLoaded(msg reviewStatusesLoadedMsg) {
	if !m.diff.active || msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel || msg.gen != m.diff.reviewStatusGen {
		return
	}
	if msg.err != nil {
		m.errBar.text = "loading review statuses: " + msg.err.Error()
		return
	}
	key := m.reviewKey()
	notes := m.diff.annotations[key]
	for i := range notes {
		if handled, ok := msg.handled[notes[i].id]; ok {
			notes[i].handled = handled
		}
	}
	m.diff.annotations[key] = notes
}

func (m *Model) reviewStateSnapshot(key string) store.ReviewState {
	marks := make(map[string]uint64, len(m.diff.reviewed[key]))
	for path, hash := range m.diff.reviewed[key] {
		if hash != 0 {
			marks[path] = hash
		}
	}
	notes := make([]store.ReviewComment, 0, len(m.diff.annotations[key]))
	for _, note := range m.diff.annotations[key] {
		notes = append(notes, store.ReviewComment{
			ID: note.id, File: note.file, Line: note.line, Deleted: note.deleted,
			Excerpt: note.excerpt, Text: note.text, ContentHash: note.hash,
			Round: note.round, Scope: note.scope, Point: note.point, Resolved: note.handled, Outdated: note.outdated,
		})
	}
	return store.ReviewState{
		Reviewed: marks,
		Comments: notes,
		Round:    m.diff.rounds[key],
	}
}

func (m *Model) chainReviewWrite(run func() tea.Msg) tea.Cmd {
	previous := m.diff.reviewWriteDone
	done := make(chan struct{})
	m.diff.reviewWriteDone = done
	return func() tea.Msg {
		if previous != nil {
			<-previous
		}
		defer close(done)
		return run()
	}
}

func (m *Model) saveReviewStateCmd() tea.Cmd {
	if m.services.store == nil || m.diff.sessID == "" || m.diff.repoSel == "" {
		return nil
	}
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.services.store
	state := m.reviewStateSnapshot(m.reviewKey())
	return m.chainReviewWrite(func() tea.Msg {
		return reviewStateSavedMsg{sessID: sessID, repoRoot: repoRoot, err: stor.MergeReviewState(sessID, repoRoot, state)}
	})
}

func (m *Model) reviewCommentHandledCmd(commentID string, handled, previous bool) tea.Cmd {
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.services.store
	return m.chainReviewWrite(func() tea.Msg {
		found, err := stor.SetReviewCommentHandled(sessID, commentID, handled)
		return reviewCommentHandledMsg{
			sessID: sessID, repoRoot: repoRoot, commentID: commentID,
			handled: handled, previous: previous, found: found, err: err,
		}
	})
}

func (m *Model) handleReviewStateSaved(msg reviewStateSavedMsg) {
	if msg.err != nil && msg.sessID == m.diff.sessID && msg.repoRoot == m.diff.repoSel {
		m.errBar.text = "saving review state: " + msg.err.Error()
	}
}

func (m *Model) handleReviewCommentHandled(msg reviewCommentHandledMsg) {
	if msg.err == nil && msg.found {
		return
	}
	key := msg.sessID + "\x00" + msg.repoRoot
	notes := m.diff.annotations[key]
	for i := range notes {
		if notes[i].id == msg.commentID && notes[i].handled == msg.handled {
			notes[i].handled = msg.previous
		}
	}
	m.diff.annotations[key] = notes
	if msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel {
		return
	}
	if msg.err != nil {
		m.errBar.text = "updating review comment: " + msg.err.Error()
	} else {
		m.errBar.text = "review comment no longer exists"
	}
}

func (m *Model) reviewSendCmd(req reviewSendRequest) tea.Cmd {
	sessID, repoRoot, stor, tmuxDriver := m.diff.sessID, m.diff.repoSel, m.services.store, m.services.tmux
	return m.chainReviewWrite(func() tea.Msg {
		msg := reviewSendFinishedMsg{
			sessID: sessID, repoRoot: repoRoot, commentIDs: req.commentIDs,
			previousRound: req.previousRound, round: req.round, count: req.count, sessName: req.sess.Name,
		}
		if !tmuxDriver.Exists(req.sess.ID) {
			msg.err = errors.New(deadSessionHint)
			return msg
		}
		if err := stor.MergeReviewState(sessID, repoRoot, req.state); err != nil {
			msg.err = fmt.Errorf("saving review round: %w", err)
			return msg
		}
		if err := tmuxDriver.SendText(req.sess.ID, req.prompt); err != nil {
			msg.err = err
			if rollbackErr := stor.MergeReviewState(sessID, repoRoot, req.previousState); rollbackErr != nil {
				msg.err = fmt.Errorf("%w; restoring review drafts: %w", err, rollbackErr)
			}
			return msg
		}
		msg.delivered = true
		msg.ackErr = stor.SetAcked(req.sess.ID, false)
		return msg
	})
}

func (m *Model) handleReviewSendFinished(msg reviewSendFinishedMsg) {
	m.diff.reviewSendPending = false
	key := msg.sessID + "\x00" + msg.repoRoot
	if !msg.delivered {
		ids := make(map[string]bool, len(msg.commentIDs))
		for _, id := range msg.commentIDs {
			ids[id] = true
		}
		notes := m.diff.annotations[key]
		for i := range notes {
			if ids[notes[i].id] && notes[i].round == msg.round {
				notes[i].round = 0
				notes[i].point = 0
			}
		}
		m.diff.annotations[key] = notes
		if m.diff.rounds[key].Number == msg.round {
			m.diff.rounds[key] = msg.previousRound
		}
	}
	if msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel {
		return
	}
	if msg.err != nil {
		m.diff.notice = ""
		m.errBar.text = msg.err.Error()
		return
	}
	m.diff.notice = fmt.Sprintf("sent review round %d (%d %s) to %s",
		msg.round, msg.count, commentNoun(msg.count), msg.sessName)
	if msg.ackErr != nil {
		m.errBar.text = "comments sent, but clearing the alert ack failed: " + msg.ackErr.Error()
	}
	m.requestRefresh()
}
