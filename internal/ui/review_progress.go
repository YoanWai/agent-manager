package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
)

func (m *Model) toggleReviewed() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil || !fd.Loaded() || m.diffFileHidden(fd) {
		return nil
	}
	if m.services.store == nil {
		m.errBar.text = "review state is unavailable"
		return nil
	}
	marks := m.diff.reviewed[m.reviewKey()]
	if marks == nil {
		marks = map[string]uint64{}
		m.diff.reviewed[m.reviewKey()] = marks
	}
	markKey := m.reviewedMarkKey(fd.File.Path)
	if marks[markKey] != 0 {
		delete(marks, markKey)
		return m.saveReviewStateCmd()
	}
	marks[markKey] = contentHash(fd)
	stateSave := m.saveReviewStateCmd()
	// Advance to the next unreviewed file, review-queue style. The switch
	// returns the highlight command for the newly shown file; dropping it
	// would leave that file unhighlighted.
	for step := 1; step < len(m.diff.set.Files); step++ {
		next := (m.diff.fileIdx + step) % len(m.diff.set.Files)
		if marks[m.reviewedMarkKey(m.diff.set.Files[next].File.Path)] == 0 && !m.diffFileHidden(&m.diff.set.Files[next]) {
			return tea.Batch(stateSave, m.switchDiffFile(next-m.diff.fileIdx))
		}
	}
	return stateSave
}

func (m *Model) fileReviewed(path string) bool {
	return m.diff.reviewed[m.reviewKey()][m.reviewedMarkKey(path)] != 0
}

func clearStaleReviewedMarks(m *Model) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	if len(marks) == 0 {
		return false
	}
	changed := false
	for markKey, stored := range marks {
		if stored == 0 {
			continue
		}
		// A mark hashes the rendering of the scope it was taken in, so only
		// that scope can judge it stale. A scope that does not list the file
		// says nothing about it either, and the mark outlives the scope.
		scope, path, _ := strings.Cut(markKey, "\x00")
		if scope != m.diff.scope.String() {
			continue
		}
		fd := m.fileDiffByPath(path)
		if fd != nil && fd.Loaded() && contentHash(fd) != stored {
			delete(marks, markKey)
			changed = true
		}
	}
	return changed
}

func clearStaleReviewedMark(m *Model, path string) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	markKey := m.reviewedMarkKey(path)
	stored := marks[markKey]
	if stored == 0 {
		return false
	}
	fd := m.fileDiffByPath(path)
	if fd != nil && fd.Loaded() && contentHash(fd) != stored {
		delete(marks, markKey)
		return true
	}
	return false
}

// A round sent under another scope listed other files, so this scope's file
// list says nothing about whether those comments still sit on their code.
// Each comment is judged against the scope its own round was sent in.
func (m *Model) markMissingRoundCommentsOutdated() bool {
	current := m.diff.scope.String()
	// Comments saved before they carried a scope fall back to the latest
	// round's scope, the only one recorded then.
	fallback := m.diff.rounds[m.reviewKey()].Scope
	paths := make(map[string]bool, len(m.diff.set.Files))
	for i := range m.diff.set.Files {
		paths[m.diff.set.Files[i].File.Path] = true
	}
	notes := m.diff.annotations[m.reviewKey()]
	changed := false
	for i := range notes {
		if notes[i].round == 0 || notes[i].outdated || paths[notes[i].file] {
			continue
		}
		scope := notes[i].scope
		if scope == "" {
			scope = fallback
		}
		if scope != "" && scope != current {
			continue
		}
		notes[i].outdated = true
		changed = true
	}
	return changed
}

// reanchorAnnotationsFor re-points saved comments at the line that still
// carries their excerpt after a reload shifted line numbers (the agent
// edits while the user reviews), choosing the nearest match. A comment
// whose line vanished entirely keeps its number as the best guess.
func (m *Model) reanchorAnnotationsFor(path string) bool {
	notes := m.diff.annotations[m.reviewKey()]
	changed := false
	for i := range notes {
		note := &notes[i]
		if path != "" && note.file != path {
			continue
		}
		// A comment's line and hash track the rendering of its own scope;
		// another scope's rendering would move it onto lines it was never
		// made against. Comments saved before they carried a scope fall
		// back to the latest round's scope, the only one recorded then.
		scope := note.scope
		if scope == "" && note.round > 0 {
			scope = m.diff.rounds[m.reviewKey()].Scope
		}
		if scope != "" && scope != m.diff.scope.String() {
			continue
		}
		fd := m.fileDiffByPath(note.file)
		if fd == nil {
			continue
		}
		currentHash := contentHash(fd)
		if note.hash != 0 && note.hash == currentHash {
			if note.outdated {
				note.outdated = false
				changed = true
			}
			continue
		}
		// A blank line's excerpt is empty and would match every blank line;
		// only a distinctive excerpt can re-anchor.
		if note.excerpt == "" {
			if note.round > 0 && !note.outdated {
				note.outdated = true
				changed = true
			}
			continue
		}
		matches, target := 0, 0
		for _, line := range fd.Lines {
			if line.Kind == diff.Gap {
				continue
			}
			num, deleted := annotationLine(line)
			if deleted == note.deleted && excerptOf(line.Text) == note.excerpt {
				matches++
				target = num
			}
		}
		// Move the note only when the excerpt pins exactly one line and no
		// other note already sits there; zero, several, or contested matches
		// are ambiguous, so the stored line stays put rather than snapping to
		// the wrong line or collapsing two comments onto one.
		if matches == 1 && !m.annotationOccupies(note.file, target, note.deleted, i) {
			if note.line != target || note.hash != currentHash || note.outdated {
				note.line = target
				note.hash = currentHash
				note.outdated = false
				changed = true
			}
		} else if note.round > 0 && !note.outdated {
			note.outdated = true
			changed = true
		}
	}
	return changed
}

func (m *Model) fileDiffByPath(path string) *diff.FileDiff {
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == path {
			return &m.diff.set.Files[i]
		}
	}
	return nil
}
