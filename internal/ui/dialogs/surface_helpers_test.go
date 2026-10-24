package dialogs

import (
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
)

// stubHost is the stack as a card reads it: a pop, a size and a card.
type stubHost struct{ popped int }

func (h *stubHost) CardSized(int, string, string, [][2]string) string { return "" }
func (h *stubHost) Size() (int, int)                                  { return 100, 40 }
func (h *stubHost) Pop()                                              { h.popped++ }

// renameStub is the root as the rename row reaches it: what a key asked for,
// recorded instead of run.
type renameStub struct {
	errs        []string
	suggestions int
	steps       int
	requests    []RenameRequest
	bases       []string
	base        string
}

func (r *renameStub) ReportErr(text string) { r.errs = append(r.errs, text) }

func (r *renameStub) ApplyPathSuggestion() tea.Cmd { r.suggestions++; return nil }

func (r *renameStub) StepBase(*RenameSurface, int) (string, tea.Cmd) {
	r.steps++
	return r.base, nil
}

func (r *renameStub) WorktreeCount() int { return 3 }

func (r *renameStub) QueueRename(request RenameRequest, base string) tea.Cmd {
	r.requests = append(r.requests, request)
	r.bases = append(r.bases, base)
	return nil
}

// moveStub is the root as the move card reaches it: the rows a submit
// placed and whether the card asked to paint.
type moveStub struct {
	placed  []Move
	groups  []string
	painted bool
}

func (r *moveStub) PlaceMove(move Move, picker *spawn.Picker) tea.Cmd {
	r.placed = append(r.placed, move)
	r.groups = append(r.groups, picker.GroupPath())
	return nil
}

func (r *moveStub) PaintMove(*spawn.Picker) string { r.painted = true; return "" }

var (
	_ surface.Host = (*stubHost)(nil)
	_ RenameRoot   = (*renameStub)(nil)
	_ MoveRoot     = (*moveStub)(nil)
)
