package review

import (
	"fmt"
	"path/filepath"
	"strings"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
)

// ApplyLoad is the only way a load can replace Review's target data. The
// target, scope, and generation checks therefore cannot be skipped by root.
func (m *Model) ApplyLoad(result LoadResult) ApplyResult {
	if !m.active || result.TargetID != m.target.ID || result.Scope != m.scope || result.Generation != m.gen {
		return ApplyResult{}
	}
	accepted := ApplyResult{Accepted: true}
	if m.annotationOpen || m.sendConfirm {
		m.loading = false
		return accepted
	}
	m.loading = false
	m.fingerprint = result.Fingerprint
	if result.Err != nil {
		m.errText = result.Err.Error()
		m.set = diff.Set{}
		m.worktrees = nil
		if len(result.RepoRoots) > 0 {
			m.repoRoots = append([]string(nil), result.RepoRoots...)
			m.repoSel = result.RepoRoot
		}
		return accepted
	}
	m.errText = ""
	m.repoRoots = append([]string(nil), result.RepoRoots...)
	m.repoSel = result.RepoRoot
	m.worktrees = append([]Worktree(nil), result.Worktrees...)
	restored := false
	if result.SavedErr != nil {
		accepted.Error = "loading review state: " + result.SavedErr.Error()
	} else if result.SavedLoaded {
		restored = m.restore(result.Saved)
	}
	if result.MissingRepo != "" {
		accepted.Error = fmt.Sprintf("picked or declared repo %s is no longer under the session directory", filepath.Base(result.MissingRepo))
		accepted.ForgetPreferredRepo = result.MissingRepo
	}
	previousPath := ""
	if fd := m.currentFile(); fd != nil {
		previousPath = fd.File.Path
	}
	m.set = result.Set.Clone()
	stateChanged := m.clearStaleMarks()
	stateChanged = m.markMissingCommentsOutdated() || stateChanged
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	if result.Refresh || restored {
		m.reanchor = make(map[string]bool)
		for _, note := range m.annotations[m.reviewKey()] {
			m.reanchor[note.file] = true
		}
	}
	m.fileIdx = 0
	for i := range m.set.Files {
		if m.set.Files[i].File.Path == previousPath {
			m.fileIdx = i
			break
		}
	}
	m.fileIdx = m.nextShownFile(m.fileIdx, 1)
	m.clampCursor()
	if request, ok := m.requestFile(m.fileIdx); ok {
		accepted.Requests.Files = append(accepted.Requests.Files, request)
	} else if request, ok := m.requestHighlight(); ok {
		accepted.Requests.Highlight = &request
	}
	if result.Refresh || restored {
		stateful := make(map[string]bool)
		for markKey, hash := range m.reviewed[m.reviewKey()] {
			scope, path, _ := strings.Cut(markKey, "\x00")
			if hash != 0 && scope == m.scope.String() {
				stateful[path] = true
			}
		}
		for _, note := range m.annotations[m.reviewKey()] {
			stateful[note.file] = true
		}
		for i := range m.set.Files {
			if stateful[m.set.Files[i].File.Path] {
				if request, ok := m.requestFile(i); ok {
					accepted.Requests.Files = append(accepted.Requests.Files, request)
				}
			}
		}
	}
	if stateChanged {
		accepted.Requests.Save = m.saveRequest()
	}
	accepted.Requests.StartupTick = true
	return accepted
}

func (m *Model) ApplyFile(result FileResult) ApplyResult {
	if !m.active || result.TargetID != m.target.ID || result.Scope != m.scope ||
		result.Generation != m.gen || result.RepoRoot != m.set.Repo.Root {
		return ApplyResult{}
	}
	if result.Index < 0 || result.Index >= len(m.set.Files) || m.set.Files[result.Index].File.Path != result.Path {
		return ApplyResult{}
	}
	delete(m.fileLoading, result.Index)
	m.set.Files[result.Index] = result.File.Clone()
	stateChanged := m.clearStaleMark(result.Path)
	if m.reanchor[result.Path] {
		stateChanged = m.reanchorAnnotations(result.Path) || stateChanged
		delete(m.reanchor, result.Path)
	}
	accepted := ApplyResult{Accepted: true}
	if stateChanged {
		accepted.Requests.Save = m.saveRequest()
	}
	if result.Index != m.fileIdx {
		return accepted
	}
	if target := m.nextShownFile(m.fileIdx, 1); target != m.fileIdx {
		accepted.Requests = mergeRequests(accepted.Requests, m.SwitchFile(target-m.fileIdx))
		return accepted
	}
	m.clampCursor()
	if request, ok := m.requestHighlight(); ok {
		accepted.Requests.Highlight = &request
	}
	return accepted
}

func (m *Model) StatusRequest() (StatusRequest, bool) {
	if !m.active || m.target.ID == "" || m.repoSel == "" {
		return StatusRequest{}, false
	}
	m.statusGen++
	return StatusRequest{TargetID: m.target.ID, RepoRoot: m.repoSel, Generation: m.statusGen}, true
}

func (m *Model) ApplyStatus(result StatusResult) ApplyResult {
	if !m.active || result.TargetID != m.target.ID || result.RepoRoot != m.repoSel || result.Generation != m.statusGen {
		return ApplyResult{}
	}
	accepted := ApplyResult{Accepted: true}
	if result.Err != nil {
		accepted.Error = "loading review statuses: " + result.Err.Error()
		return accepted
	}
	key := m.reviewKey()
	notes := m.annotations[key]
	for i := range notes {
		if handled, ok := result.Handled[notes[i].id]; ok {
			notes[i].handled = handled
		}
	}
	m.annotations[key] = notes
	return accepted
}

func (m *Model) ApplySave(result SaveResult) ApplyResult {
	accepted := ApplyResult{Accepted: true}
	if result.Err != nil && result.TargetID == m.target.ID && result.RepoRoot == m.repoSel {
		accepted.Error = "saving review state: " + result.Err.Error()
	}
	return accepted
}

func (m *Model) ApplyHighlight(result HighlightResult) bool {
	m.putHighlight(result.Key, result.Highlight)
	if m.highlightPending == result.Key {
		m.highlightPending = HighlightKey{}
	}
	return result.Key.TargetID == m.target.ID && result.Key.Scope == m.scope
}

func (m *Model) requestHighlight() (HighlightRequest, bool) {
	fd := m.currentFile()
	if fd == nil || !fd.Loaded() || fd.Binary || fd.Err != nil || len(fd.Lines) == 0 {
		return HighlightRequest{}, false
	}
	key := HighlightKey{TargetID: m.target.ID, Scope: m.scope, Path: fd.File.Path, Hash: ContentHash(fd)}
	if m.highlights[key] != nil || m.highlightPending == key {
		return HighlightRequest{}, false
	}
	m.highlightPending = key
	return HighlightRequest{Key: key, File: fd.Clone()}, true
}

func (m *Model) EnsureHighlight() (HighlightRequest, bool) { return m.requestHighlight() }

func (m *Model) EnsureCurrentFile() Requests {
	requests := Requests{}
	if request, ok := m.requestFile(m.fileIdx); ok {
		requests.Files = append(requests.Files, request)
	} else if request, ok := m.requestHighlight(); ok {
		requests.Highlight = &request
	}
	return requests
}

func (m *Model) requestFile(index int) (FileRequest, bool) {
	if index < 0 || index >= len(m.set.Files) {
		return FileRequest{}, false
	}
	fd := &m.set.Files[index]
	if fd.Loaded() {
		return FileRequest{}, false
	}
	if m.fileLoading == nil {
		m.fileLoading = make(map[int]bool)
	}
	if m.fileLoading[index] {
		return FileRequest{}, false
	}
	m.fileLoading[index] = true
	snapshot := m.set.Clone()
	snapshot.Files = []diff.FileDiff{fd.Clone()}
	return FileRequest{
		TargetID: m.target.ID, Scope: m.scope, Generation: m.gen,
		RepoRoot: m.set.Repo.Root, Index: index, Path: fd.File.Path, Set: snapshot,
	}, true
}

func (m *Model) nextShownFile(index, dir int) int {
	count := len(m.set.Files)
	if count == 0 {
		return index
	}
	for step := 0; step < count; step++ {
		candidate := ((index+dir*step)%count + count) % count
		if !m.fileHidden(&m.set.Files[candidate]) {
			return candidate
		}
	}
	return index
}

func (m *Model) fileHidden(fd *diff.FileDiff) bool {
	return m.codeOnly && isNonCode(fd)
}

func (m *Model) clampCursor() {
	total := 0
	if fd := m.currentFile(); fd != nil {
		total = m.rowCount(fd)
	}
	if m.cursorLine >= total {
		m.cursorLine = total - 1
	}
	if m.cursorLine < 0 {
		m.cursorLine = 0
	}
	if m.scroll >= total {
		m.scroll = total - 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m Model) rowCount(fd *diff.FileDiff) int {
	if m.sideBySide {
		return len(fd.SideBySideRows())
	}
	return len(fd.Lines)
}
