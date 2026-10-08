package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeGroupFormHost drives the New Group form without a root model.
type fakeGroupFormHost struct {
	errs   []string
	paths  pathComplete
	parent string
}

func (h *fakeGroupFormHost) reportErr(text string) { h.errs = append(h.errs, text) }

func (h *fakeGroupFormHost) clearErr() {}

func (h *fakeGroupFormHost) pathSuggestions() *pathComplete { return &h.paths }

func (h *fakeGroupFormHost) selectedGroupPath() string { return h.parent }

func (h *fakeGroupFormHost) viewGroupPicker() string { return "" }

func (h *fakeGroupFormHost) groupBase(string) string { return "" }

func (h *fakeGroupFormHost) groupDirCandidates(group string) []string {
	return []string{"/work/" + group}
}

// The group form validates its name and builds the group under the
// picked parent; the parent and base steps go to the root.
func TestGroupFormDialogThroughAFakeHost(t *testing.T) {
	h := &fakeGroupFormHost{parent: "backend"}
	name := textField("group-name", 60)
	name.Focus()
	d := &groupFormDialog{groupForm{name: name, path: textField("", 400), pathAuto: true, gen: 4}}
	if _, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); request.create != nil || len(h.errs) != 1 {
		t.Fatalf("an empty name submitted: %+v %v", request, h.errs)
	}
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("api/v2"), Paste: true})
	d.focusStep(h, 1)
	if _, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyRight}); request.parent != 1 {
		t.Fatalf("right on the parent row asked for %+v", request)
	}
	d.focusStep(h, 2)
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyLeft})
	if d.worktreeIndex != 2 {
		t.Fatalf("left from inherit = %d, want off", d.worktreeIndex)
	}
	d.focusStep(h, 1)
	if _, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyLeft}); request.base != -1 {
		t.Fatalf("left on the base row asked for %+v", request)
	}
	_, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	want := groupRequest{path: "backend/api-v2", worktree: "off", gen: 4, draftName: "api/v2", fallbacks: []string{"/work/backend"}}
	if request.create == nil || !reflect.DeepEqual(*request.create, want) {
		t.Fatalf("create = %+v, want %+v", request.create, want)
	}
	if _, request = d.handleKey(h, tea.KeyMsg{Type: tea.KeyEsc}); !request.close {
		t.Fatalf("esc asked for %+v", request)
	}
}
