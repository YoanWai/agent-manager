package review

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// Key owns Review's complete key priority. Root interprets navigation and runs
// the concrete I/O requests, but cannot bypass draft or confirmation modes.
func (m *Model) Key(msg tea.KeyMsg, ctx KeyContext) KeyResult {
	m.notice = ""
	if m.annotationOpen {
		return m.AnnotationKey(msg)
	}
	if m.sendConfirm {
		switch msg.String() {
		case "ctrl+c":
			return KeyResult{Navigation: NavigationQuit, Consumed: true}
		case "enter", "y":
			m.sendConfirm = false
			if ctx.SendError != "" {
				return KeyResult{Error: ctx.SendError, Consumed: true}
			}
			if !ctx.PersistenceAvailable {
				return KeyResult{Error: "review state is unavailable", Consumed: true}
			}
			result := m.BeginSend()
			return KeyResult{Requests: result.Requests, Error: result.Error, Consumed: true}
		case "esc", "q":
			m.sendConfirm = false
		}
		return KeyResult{Consumed: true}
	}
	height := ctx.CodeHeight
	if height < 1 {
		height = 1
	}
	result := KeyResult{Consumed: true}
	switch msg.String() {
	case "ctrl+c":
		result.Navigation = NavigationQuit
	case "q", "esc":
		result.Navigation = NavigationExit
	case "?":
		result.Navigation = NavigationHelp
	case "up", "k":
		m.MoveCursor(-1, height)
	case "down", "j":
		m.MoveCursor(1, height)
	case "ctrl+d":
		m.MoveCursor(height/2, height)
	case "ctrl+u":
		m.MoveCursor(-height/2, height)
	case "pgup":
		m.MoveCursor(-height, height)
	case "pgdown", "pgdn":
		m.MoveCursor(height, height)
	case "g":
		m.First()
	case "G":
		m.Last(height)
	case "J", "tab":
		result.Requests = m.SwitchFile(1)
	case "K", "shift+tab":
		result.Requests = m.SwitchFile(-1)
	case "n":
		m.JumpChange(1, height)
	case "N", "shift+n":
		m.JumpChange(-1, height)
	case "s":
		request, ok := m.CycleScope()
		if ok {
			result.Requests.Load = &request
			result.Requests.StartupTick = true
		}
	case "r":
		result.Navigation = NavigationRepoPicker
	case "b":
		result.Navigation = NavigationBranchPicker
	case "B":
		result.Navigation = NavigationBasePicker
	case "u":
		m.ToggleSideBySide(height)
	case "f":
		result.Requests = m.ToggleCodeOnly()
	case " ", "space":
		if !ctx.PersistenceAvailable {
			result.Error = "review state is unavailable"
		} else {
			result.Requests = m.ToggleReviewed()
		}
	case "c":
		m.OpenAnnotation()
	case "d":
		result.Requests, result.Error = m.DiscardOrToggle(ctx.PersistenceAvailable)
	case "C":
		if m.DraftCount() == 0 {
			result.Error = "no comments to send - press c on a line first"
		} else {
			m.sendConfirm = true
		}
	case "o", "f3":
		result.Requests.FileCheck = m.OpenFileRequest()
	default:
		result.Consumed = false
	}
	return result
}

func (m Model) OpenFileRequest() *FileCheckRequest {
	fd := m.currentFile()
	if fd == nil || m.fileHidden(fd) || m.set.Repo.Root == "" {
		return nil
	}
	return &FileCheckRequest{TargetID: m.target.ID, RepoRoot: m.repoSel, Generation: m.gen, Path: filepath.Join(m.set.Repo.Root, fd.File.Path)}
}

func (m *Model) ApplyFileCheck(result FileCheckResult) (string, bool) {
	req := result.Request
	if !m.active || req.TargetID != m.target.ID || req.RepoRoot != m.repoSel || req.Generation != m.gen {
		return "", false
	}
	fd := m.currentFile()
	if fd == nil || filepath.Join(m.set.Repo.Root, fd.File.Path) != req.Path {
		return "", false
	}
	return req.Path, true
}
