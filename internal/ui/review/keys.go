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
	result := KeyResult{Consumed: true}
	b.Run(m, ctx, height, &result)
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
