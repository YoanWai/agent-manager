package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func formOpen(m *Model) bool { return m.form() != nil }

// picker is the group picker on screen: the move card's, or the form's.
func picker(m *Model) *groupPicker {
	if s, ok := surfaceOf[*moveSurface](m); ok {
		return &s.groupPicker
	}
	return &m.form().groupPicker
}

// pushForm puts a bare New Session form on the stack, for a test that needs
// one open without the reads openForm starts.
func pushForm(m *Model) *spawnSurface {
	f := newSpawnSurface(formDialog{prompt: promptField()})
	m.push(f)
	return f
}

// The form opens by its key, keeps its draft through a stray click and
// closes on esc, letting go of its images.
func TestSpawnFormIsASurface(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.Update(key("n"))
	if !formOpen(m) {
		t.Fatalf("n did not open the form, err %q", m.errBar.text)
	}
	if mustTop(t, m).Dismissible() {
		t.Fatal("the form holds a draft and must not close on a click")
	}
	m.form().SetName("draft")
	clickBackdrop(t, m)
	if !formOpen(m) || m.form().Name() != "draft" {
		t.Fatal("a stray click closed the form")
	}
	image := tempImage(t, "draft.png")
	m.form().prompt.attachments = []imageAttachment{{id: 1, path: image}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if formOpen(m) || len(m.surfaces) != 0 {
		t.Fatal("esc did not close the form")
	}
	if imageExists(t, image) {
		t.Fatal("closing the form kept its image")
	}
}

// A settings read that lands after its form closed, or on a newer form, is
// dropped.
func TestStaleFormCompletionIsDropped(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openForm())
	gen := m.form().prompt.gen
	m.closeForm()
	if _, ok := m.formFor(gen); ok {
		t.Fatal("a closed form still answers its generation")
	}
	m.applyCmd(t, m.openForm())
	if _, ok := m.formFor(gen); ok {
		t.Fatal("a newer form answered an older generation")
	}
}
