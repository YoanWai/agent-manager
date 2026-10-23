package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// pushQuick puts a bare bar on the stack, for a test that needs one open
// without the settings and catalog reads openQuickMode starts.
func pushQuick(m *Model) *quickSurface {
	q := newQuickSurface(quickBar{Composer: composer.Composer{Input: textarea.New()}})
	m.push(q)
	return q
}

// The bar opens by its key, holds its draft through a stray click, and
// closes on esc, letting go of its images.
func TestQuickBarIsASurface(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.applyCmd(t, m.openQuickMode())
	if m.quick() == nil {
		t.Fatalf("the quick bar did not open, err %q", m.errBar.text)
	}
	if mustTop(t, m).Dismissible() {
		t.Fatal("the bar holds a draft and must not close on a click")
	}
	if m.cardOpen() || !m.listClear() {
		t.Fatal("the bar sits on the list; it is no card over it")
	}
	image := tempImage(t, "draft.png")
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quick() != nil || len(m.surfaces) != 0 {
		t.Fatal("esc did not close the bar")
	}
	if imageExists(t, image) {
		t.Fatal("closing the bar kept its image")
	}
}

// A settings read that lands after its bar closed, or on a newer bar, is
// dropped.
func TestStaleQuickCompletionIsDropped(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openQuickMode())
	gen := m.quick().Gen
	m.closeQuick()
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a closed bar still answers its generation")
	}
	m.applyCmd(t, m.openQuickMode())
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a newer bar answered an older generation")
	}
}
