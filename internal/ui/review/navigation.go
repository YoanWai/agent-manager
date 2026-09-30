package review

import (
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/diff"
)

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

func isNonCode(fd *diff.FileDiff) bool {
	name := filepath.Base(fd.File.Path)
	return fd.Binary || fd.Stat.Binary || nonCodeNames[name] || nonCodeExts[strings.ToLower(filepath.Ext(name))]
}

func (m Model) scrollKey(path string) string {
	return m.reviewKey() + "\x00" + m.scope.String() + "\x00" + path
}

func (m Model) FileHidden(fd *diff.FileDiff) bool { return m.fileHidden(fd) }

func (m *Model) ToggleCodeOnly() Requests {
	m.codeOnly = !m.codeOnly
	target := m.nextShownFile(m.fileIdx, 1)
	if target == m.fileIdx {
		return Requests{}
	}
	return m.SwitchFile(target - m.fileIdx)
}

func (m *Model) SwitchFile(delta int) Requests {
	count := len(m.set.Files)
	if count == 0 {
		return Requests{}
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	target := m.nextShownFile((m.fileIdx+delta+count)%count, dir)
	if m.fileHidden(&m.set.Files[target]) {
		return Requests{}
	}
	if fd := m.currentFile(); fd != nil {
		m.scrollByFile[m.scrollKey(fd.File.Path)] = m.scroll
	}
	m.fileIdx = target
	fd := m.currentFile()
	m.scroll = m.scrollByFile[m.scrollKey(fd.File.Path)]
	m.cursorLine = m.scroll
	m.clampCursor()
	requests := Requests{StartupTick: true}
	if request, ok := m.requestFile(m.fileIdx); ok {
		requests.Files = append(requests.Files, request)
	} else if request, ok := m.requestHighlight(); ok {
		requests.Highlight = &request
	}
	return requests
}

func (m *Model) MoveCursor(delta, height int) {
	m.cursorLine += delta
	m.clampCursor()
	if m.cursorLine < m.scroll {
		m.scroll = m.cursorLine
	}
	if m.cursorLine >= m.scroll+height {
		m.scroll = m.cursorLine - height + 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *Model) JumpChange(delta, height int) {
	fd := m.currentFile()
	if fd == nil || len(fd.Changes) == 0 {
		return
	}
	line := m.CursorDiffLine()
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
	m.SetCursorDiffLine(target, height)
}

func (m Model) CursorDiffLine() int {
	fd := m.currentFile()
	if fd == nil {
		return 0
	}
	if m.sideBySide {
		rows := fd.SideBySideRows()
		if m.cursorLine < len(rows) {
			row := rows[m.cursorLine]
			if row.Right >= 0 {
				return row.Right
			}
			return row.Left
		}
		return 0
	}
	return m.cursorLine
}

func (m *Model) SetCursorDiffLine(lineIdx, height int) {
	fd := m.currentFile()
	if fd == nil {
		return
	}
	if m.sideBySide {
		for i, row := range fd.SideBySideRows() {
			if row.Left == lineIdx || row.Right == lineIdx {
				m.cursorLine = i
				break
			}
		}
	} else {
		m.cursorLine = lineIdx
	}
	m.clampCursor()
	if m.cursorLine < m.scroll || m.cursorLine >= m.scroll+height {
		m.scroll = m.cursorLine - height/2
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m *Model) ToggleSideBySide(height int) {
	lineIdx := m.CursorDiffLine()
	m.sideBySide = !m.sideBySide
	m.SetCursorDiffLine(lineIdx, height)
}

func (m *Model) First() {
	m.cursorLine = 0
	m.scroll = 0
}

func (m *Model) Last(height int) {
	if fd := m.currentFile(); fd != nil {
		m.cursorLine = m.rowCount(fd) - 1
		m.MoveCursor(0, height)
	}
}

func (m *Model) Wheel(delta, height int) {
	m.MoveCursor(delta, height)
}

// PrepareViewport applies the wrapped-row visibility policy before the frame
// is painted. spans contains the rendered row height for every logical row.
func (m *Model) PrepareViewport(spans []int, height int) {
	total := len(spans)
	cursor := m.cursorLine
	if cursor > total-1 {
		cursor = total - 1
	}
	if cursor < 0 {
		return
	}
	if m.scroll > cursor {
		m.scroll = cursor
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	for m.scroll < cursor {
		used := 0
		if m.scroll > 0 {
			used++
		}
		if cursor < total-1 {
			used++
		}
		fits := true
		for i := m.scroll; i <= cursor; i++ {
			used += spans[i]
			if used > height {
				fits = false
				break
			}
		}
		if fits {
			break
		}
		m.scroll++
	}
}
