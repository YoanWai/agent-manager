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
	b, ok := bindingFor(msg.String())
	if !ok {
		return KeyResult{}
	}
	return m.run(b.Action, ctx, height)
}

func (m *Model) run(act action, ctx KeyContext, height int) KeyResult {
	result := KeyResult{Consumed: true}
	switch act {
	case actClose:
		result.Navigation = NavigationExit
	case actHelp:
		result.Navigation = NavigationHelp
	case actLineUp:
		m.MoveCursor(-1, height)
	case actLineDown:
		m.MoveCursor(1, height)
	case actHalfDown:
		m.MoveCursor(height/2, height)
	case actHalfUp:
		m.MoveCursor(-height/2, height)
	case actPageUp:
		m.MoveCursor(-height, height)
	case actPageDown:
		m.MoveCursor(height, height)
	case actTop:
		m.First()
	case actBottom:
		m.Last(height)
	case actNextFile:
		result.Requests = m.SwitchFile(1)
	case actPrevFile:
		result.Requests = m.SwitchFile(-1)
	case actNextChange:
		m.JumpChange(1, height)
	case actPrevChange:
		m.JumpChange(-1, height)
	case actScope:
		if request, ok := m.CycleScope(); ok {
			result.Requests.Load = &request
			result.Requests.StartupTick = true
		}
	case actRepo:
		result.Navigation = NavigationRepoPicker
	case actBranch:
		result.Navigation = NavigationBranchPicker
	case actTarget:
		result.Navigation = NavigationBasePicker
	case actLayout:
		m.ToggleSideBySide(height)
	case actCodeOnly:
		result.Requests = m.ToggleCodeOnly()
	case actReviewed:
		if !ctx.PersistenceAvailable {
			result.Error = "review state is unavailable"
		} else {
			result.Requests = m.ToggleReviewed()
		}
	case actComment:
		m.OpenAnnotation()
	case actDiscard:
		result.Requests, result.Error = m.DiscardOrToggle(ctx.PersistenceAvailable)
	case actSend:
		if m.DraftCount() == 0 {
			result.Error = "no comments to send - press c on a line first"
		} else {
			m.sendConfirm = true
		}
	case actOpenFile:
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
