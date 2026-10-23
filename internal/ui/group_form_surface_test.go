package ui

import (
	"strings"
	"testing"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	tea "github.com/charmbracelet/bubbletea"
)

// groupSurface is the open New Group card; a test that reads it without one
// open is wrong, so it panics rather than returning a half-built one.
func (m *Model) groupSurface() *groupFormSurface {
	s, ok := surfaceOf[*groupFormSurface](m)
	if !ok {
		panic("the New Group card is not open")
	}
	return s
}

func (m *Model) groupFormOpen() bool {
	_, ok := surfaceOf[*groupFormSurface](m)
	return ok
}

// pressGroupKey types into the card through Update, the way a terminal does.
func (m *Model) pressGroupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) { return m.Update(msg) }

func (m *Model) submitGroupForm() (tea.Model, tea.Cmd) {
	return m.submitGroupFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

// submitGroupFormWithReader presses the card's submit with a preflight the
// test controls, which Update cannot inject.
func (m *Model) submitGroupFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	s := m.groupSurface()
	key := s.form.Submit(s.parentPath())
	if key.Action == uidialogs.GroupFormInvalid {
		m.reportErr(key.Err)
		return m, nil
	}
	return m, m.handleGroupFormSubmit(groupFormSubmitMsg{request: key.Request, base: s.base, remote: s.remote}, reader)
}

func (m *Model) viewGroupForm() string { return preparedView(m) }

func openGroupFormModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openGroupForm()
	return m
}

func TestGroupFormOpensFromTheNewGroupKey(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	if !m.groupFormOpen() {
		t.Fatalf("the new group key did not open the card, surfaces = %d", len(m.surfaces))
	}
	if !strings.Contains(preparedView(m), "New Group") {
		t.Fatal("the frame does not show the card")
	}
}

func TestGroupFormEscClosesAndKeepsNothingBehind(t *testing.T) {
	t.Parallel()
	m := openGroupFormModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.groupFormOpen() || len(m.surfaces) != 0 {
		t.Fatalf("esc left %d surfaces", len(m.surfaces))
	}
}

// The form holds a typed draft, so a stray click off the card keeps it.
func TestGroupFormKeepsItsDraftThroughAStrayClick(t *testing.T) {
	t.Parallel()
	m := openGroupFormModel(t)
	if m.top0().Dismissible() {
		t.Fatal("a form holding a draft says a stray click closes it")
	}
	m.groupSurface().form.SetName("draft")
	m = clickBackdrop(t, m)
	if !m.groupFormOpen() || m.groupSurface().form.Name() != "draft" {
		t.Fatal("a click off the card lost the draft")
	}
}

func (m *Model) top0() surface {
	s, _ := m.top()
	return s
}

// A completion for a form that has since closed is dropped, and one for a
// form reopened in its place carries another generation.
func TestGroupFormCompletionsAreFenced(t *testing.T) {
	t.Parallel()
	m := openGroupFormModel(t)
	stale := groupRequest{path: "x", gen: m.groupSurface().form.Gen(), draftName: "", draftDir: m.groupSurface().form.Dir()}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m.applyGroupEffect(stale, groupEffectResult{path: "x"}, nil)
	if m.groupFormOpen() {
		t.Fatal("a completion opened or kept a popped form")
	}
	m.openGroupForm()
	m.applyGroupEffect(stale, groupEffectResult{path: "x"}, nil)
	if !m.groupFormOpen() {
		t.Fatal("a stale completion closed the form that replaced it")
	}
}

func TestGroupFormBaseStepForAClosedFormIsDropped(t *testing.T) {
	t.Parallel()
	m := openGroupFormModel(t)
	gen := m.groupSurface().form.Gen()
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd := m.handleGroupBaseAsk(groupBaseAskMsg{gen: gen, delta: 1}); cmd != nil {
		t.Fatal("a base step ran for a form that is gone")
	}
}

// applyBaseStep plays a base key's two hops: the ask the card sends the root,
// and the branch read the root answers it with.
func (m *Model) applyBaseStep(t *testing.T, step tea.Cmd) {
	t.Helper()
	read := m.stepCmd(t, step)
	if read == nil {
		return
	}
	m.applyTestMsg(t, read())
}
