package review

import (
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

var localKeyCommands = map[string]string{
	"ctrl+c": "ctrl+c", "q": "q", "esc": "q",
	"up": "up", "k": "up", "down": "down", "j": "down",
	"ctrl+d": "ctrl+d", "ctrl+u": "ctrl+u", "pgup": "pgup", "pgdn": "pgdn",
	"g": "g", "G": "G", "J": "tab", "tab": "tab", "K": "shift+tab", "shift+tab": "shift+tab",
	"n": "n", "N": "N", "s": "s", "r": "r", "b": "b", "B": "B", "u": "u", "f": "f",
	"space": "space", "c": "c", "d": "d", "C": "C", "o": "o", "f3": "o",
}

func EffectiveHelpBinding(configured keybind.Binding) keybind.Binding {
	var keys []keybind.Key
	for _, key := range configured.Keys() {
		if localKeyCommands[keybind.Normalize(key.Tea())] == "" {
			keys = append(keys, key)
		}
	}
	return keybind.Keys(keys...)
}

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
	key := keybind.Normalize(msg.String())
	height := ctx.CodeHeight
	if height < 1 {
		height = 1
	}
	result := KeyResult{Consumed: true}
	switch localKeyCommands[key] {
	case "ctrl+c":
		result.Navigation = NavigationQuit
	case "q":
		result.Navigation = NavigationExit
	case "up":
		m.MoveCursor(-1, height)
	case "down":
		m.MoveCursor(1, height)
	case "ctrl+d":
		m.MoveCursor(height/2, height)
	case "ctrl+u":
		m.MoveCursor(-height/2, height)
	case "pgup":
		m.MoveCursor(-height, height)
	case "pgdn":
		m.MoveCursor(height, height)
	case "g":
		m.First()
	case "G":
		m.Last(height)
	case "tab":
		result.Requests = m.SwitchFile(1)
	case "shift+tab":
		result.Requests = m.SwitchFile(-1)
	case "n":
		m.JumpChange(1, height)
	case "N":
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
	case "space":
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
	case "o":
		result.Requests.FileCheck = m.OpenFileRequest()
	default:
		if ctx.HelpKey.Has(key) {
			result.Navigation = NavigationHelp
		} else {
			result.Consumed = false
		}
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
