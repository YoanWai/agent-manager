package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"path/filepath"
	"strings"
)

func (m *Model) currentFileDiff() *diff.FileDiff {
	if m.diff.fileIdx < 0 || m.diff.fileIdx >= len(m.diff.set.Files) {
		return nil
	}
	return &m.diff.set.Files[m.diff.fileIdx]
}

func (m *Model) currentHL() *fileHL {
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	return m.diff.hl.get(hlKey{sessID: m.diff.sessID, scope: m.diff.scope, path: fd.File.Path, hash: contentHash(fd)})
}

// scrollKey scopes a file's saved scroll to the session, repo, and scope it
// was taken in, so positions never leak across sessions, repos, or diff scopes.
func (m *Model) scrollKey(path string) string {
	return m.reviewKey() + "\x00" + m.diff.scope.String() + "\x00" + path
}

// nonCodeExts and nonCodeNames name the files a review takes in whole rather
// than line by line: images, fonts, archives, compiled artifacts, and the lock
// files a package manager rewrites wholesale.
var nonCodeExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".svg": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".eot": true, ".wasm": true, ".bin": true, ".exe": true,
	".dll": true, ".so": true, ".dylib": true, ".o": true, ".a": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".avi": true, ".mov": true, ".pdf": true, ".lock": true,
	".class": true, ".pyc": true,
}

var nonCodeNames = map[string]bool{
	"package-lock.json": true, "pnpm-lock.yaml": true, "go.sum": true,
}

func nonCodePath(repoPath string) bool {
	name := filepath.Base(repoPath)
	return nonCodeNames[name] || nonCodeExts[strings.ToLower(filepath.Ext(name))]
}

// diffFileHidden reports whether the code-only filter drops a file from the
// review's file list. The name is what settles the verdict before the toggle
// is ever pressed: git's numstat never classifies an untracked path, and the
// loader sniffs for NUL only once the cursor reaches a file. Git's own binary
// verdict and that sniff then catch a blob whose name gives nothing away.
func (m *Model) diffFileHidden(fd *diff.FileDiff) bool {
	return m.diff.codeOnly && (fd.Binary || fd.Stat.Binary || nonCodePath(fd.File.Path))
}

// nextShownFile walks from index in the direction dir to the first file the
// code-only filter leaves in the list, and returns index untouched when the
// filter hides every file.
func (m *Model) nextShownFile(index, dir int) int {
	count := len(m.diff.set.Files)
	if count == 0 {
		return index
	}
	for step := 0; step < count; step++ {
		candidate := ((index+dir*step)%count + count) % count
		if !m.diffFileHidden(&m.diff.set.Files[candidate]) {
			return candidate
		}
	}
	return index
}

// toggleCodeOnly hides or shows the non-code files, carrying the selection to
// a file the list still shows.
func (m *Model) toggleCodeOnly() tea.Cmd {
	m.diff.codeOnly = !m.diff.codeOnly
	target := m.nextShownFile(m.diff.fileIdx, 1)
	if target == m.diff.fileIdx {
		return nil
	}
	return m.switchDiffFile(target - m.diff.fileIdx)
}

func (m *Model) switchDiffFile(delta int) tea.Cmd {
	count := len(m.diff.set.Files)
	if count == 0 {
		return nil
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	target := m.nextShownFile((m.diff.fileIdx+delta+count)%count, dir)
	if m.diffFileHidden(&m.diff.set.Files[target]) {
		return nil
	}
	if fd := m.currentFileDiff(); fd != nil {
		m.diff.scrollByFile[m.scrollKey(fd.File.Path)] = m.diff.scroll
	}
	m.diff.fileIdx = target
	fd := m.currentFileDiff()
	m.diff.scroll = m.diff.scrollByFile[m.scrollKey(fd.File.Path)]
	m.diff.cursorLine = m.diff.scroll
	m.clampDiffCursor()
	return tea.Batch(m.loadCurrentDiffFile(), m.startStartupTick())
}

func (m *Model) clampDiffCursor() {
	fd := m.currentFileDiff()
	total := 0
	if fd != nil {
		total = m.diffRowCount(fd)
	}
	if m.diff.cursorLine >= total {
		m.diff.cursorLine = total - 1
	}
	if m.diff.cursorLine < 0 {
		m.diff.cursorLine = 0
	}
	if m.diff.scroll >= total {
		m.diff.scroll = total - 1
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// diffRowCount is the navigable row count for the active layout.
func (m *Model) diffRowCount(fd *diff.FileDiff) int {
	if m.diff.sideBySide && m.mode == modeDiff {
		return len(fd.SideBySideRows())
	}
	return len(fd.Lines)
}

// moveDiffCursor moves the fullscreen line cursor, dragging the viewport
// along when the cursor leaves it.
func (m *Model) moveDiffCursor(delta int, height int) {
	m.diff.cursorLine += delta
	m.clampDiffCursor()
	if m.diff.cursorLine < m.diff.scroll {
		m.diff.scroll = m.diff.cursorLine
	}
	if m.diff.cursorLine >= m.diff.scroll+height {
		m.diff.scroll = m.diff.cursorLine - height + 1
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// jumpChange moves the cursor to the next or previous change block.
func (m *Model) jumpChange(delta int) {
	fd := m.currentFileDiff()
	if fd == nil || len(fd.Changes) == 0 {
		return
	}
	line := m.cursorDiffLine()
	target := -1
	if delta > 0 {
		for _, start := range fd.Changes {
			if start > line {
				target = start
				break
			}
		}
		if target < 0 {
			target = fd.Changes[0]
		}
	} else {
		for i := len(fd.Changes) - 1; i >= 0; i-- {
			if fd.Changes[i] < line {
				target = fd.Changes[i]
				break
			}
		}
		if target < 0 {
			target = fd.Changes[len(fd.Changes)-1]
		}
	}
	m.setCursorDiffLine(target)
}

// cursorDiffLine maps the cursor to a Lines index in either layout.
func (m *Model) cursorDiffLine() int {
	fd := m.currentFileDiff()
	if fd == nil {
		return 0
	}
	if m.diff.sideBySide && m.mode == modeDiff {
		rows := fd.SideBySideRows()
		if m.diff.cursorLine < len(rows) {
			row := rows[m.diff.cursorLine]
			if row.Right >= 0 {
				return row.Right
			}
			return row.Left
		}
		return 0
	}
	return m.diff.cursorLine
}

func (m *Model) setCursorDiffLine(lineIdx int) {
	fd := m.currentFileDiff()
	if fd == nil {
		return
	}
	if m.diff.sideBySide && m.mode == modeDiff {
		for i, row := range fd.SideBySideRows() {
			if row.Left == lineIdx || row.Right == lineIdx {
				m.diff.cursorLine = i
				break
			}
		}
	} else {
		m.diff.cursorLine = lineIdx
	}
	m.clampDiffCursor()
	height := m.diffCodeHeight()
	if m.diff.cursorLine < m.diff.scroll || m.diff.cursorLine >= m.diff.scroll+height {
		m.diff.scroll = m.diff.cursorLine - height/2
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// diffCodeHeight is the code viewport height in fullscreen review. It
// mirrors viewDiffFull's layout math, including a footer that wraps onto
// extra lines in narrow terminals. Two rows are reserved for the overflow
// indicators (↑ N more / ↓ N more) so the cursor never lands on them.
func (m *Model) diffCodeHeight() int {
	height := m.height - 6 - lipgloss.Height(m.viewDiffFooter())
	if m.diff.annotating {
		height -= m.diffAnnBarRows() + 1
	}
	height -= 2
	if height < 1 {
		height = 1
	}
	return height
}

const annotationInputMaxRows = 5

func (m *Model) diffAnnBarRows() int {
	if !m.diff.annotating {
		return 0
	}
	_, codeWidth := m.diffPaneWidths()
	return 1 + m.annotationInputHeight(codeWidth-2*contentGutter)
}

func (m *Model) annotationInputHeight(width int) int {
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	n := len(wrapTinted(m.diff.annInput.Value(), nil, "", "", inner))
	if n < 1 {
		n = 1
	}
	if n > annotationInputMaxRows {
		n = annotationInputMaxRows
	}
	return n
}

func (m *Model) diffPaneWidths() (fileWidth, codeWidth int) {
	fileWidth = max(m.width*24/100, diffFileRailWidth)
	// The file rail keeps its share only while the code pane still has one:
	// a terminal too narrow for both gives the rail what is left over.
	if m.width-fileWidth-diffPaneSeam < diffCodeMinWidth {
		fileWidth = max(m.width-diffPaneSeam-diffCodeMinWidth, 0)
	}
	return fileWidth, max(m.width-fileWidth-diffPaneSeam, 0)
}
