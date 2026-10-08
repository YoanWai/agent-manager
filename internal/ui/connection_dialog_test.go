package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeConnectionHost struct {
	mode  mode
	err   string
	list  []remote.Connection
	title string
}

func (h *fakeConnectionHost) reportErr(text string)               { h.err = text }
func (h *fakeConnectionHost) clearErr()                           { h.err = "" }
func (h *fakeConnectionHost) setMode(next mode)                   { h.mode = next }
func (h *fakeConnectionHost) connectionList() []remote.Connection { return h.list }
func (h *fakeConnectionHost) card(title, body string, _ [][2]string) string {
	h.title = title
	return title + "\n" + body
}

func typeConnection(d *connectionDialog, h connectionDialogHost, text string) {
	for _, r := range text {
		d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestConnectionDialogAddsAValidConnection(t *testing.T) {
	h := &fakeConnectionHost{mode: modeList}
	var d connectionDialog
	d.open(h, remote.Connection{})
	if h.mode != modeConnection {
		t.Fatalf("mode = %v, want the dialog open", h.mode)
	}
	typeConnection(&d, h, "box")
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyTab})
	typeConnection(&d, h, "me@box.lan")
	_, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request == nil {
		t.Fatalf("enter built no request; status %q", h.err)
	}
	want := connectionRequest{op: connectionAdd, next: store.Connection{Name: "box", Destination: "me@box.lan"}, gen: d.gen}
	if *request != want {
		t.Fatalf("request = %+v, want %+v", *request, want)
	}
	if view := d.view(h); h.title != "⇄ New SSH Connection" || !strings.Contains(view, "me@box.lan") {
		t.Fatalf("view = %q", view)
	}
}

func TestConnectionDialogTypesANameThatSpellsAKey(t *testing.T) {
	h := &fakeConnectionHost{mode: modeList}
	var d connectionDialog
	d.open(h, remote.Connection{})
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("down")})
	if d.focus != cfName || d.name.Value() != "down" {
		t.Fatalf("focus %v, name %q: a fast-typed name moved the focus", d.focus, d.name.Value())
	}
}

func TestConnectionDialogRefusesWhatValidationRefuses(t *testing.T) {
	h := &fakeConnectionHost{list: []remote.Connection{{Name: "box", Destination: "me@box"}}}
	for _, tc := range []struct{ name, destination string }{
		{"", "me@box"},
		{"other", ""},
		{"box", "me@elsewhere"},
		{"other", "-oProxyCommand=sh"},
	} {
		var d connectionDialog
		d.open(h, remote.Connection{})
		d.name.SetValue(tc.name)
		d.destination.SetValue(tc.destination)
		_, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
		want := remote.ValidateConnection(remote.Connection{Name: tc.name, Destination: tc.destination}, h.list)
		if request != nil || want == nil || h.err != want.Error() {
			t.Fatalf("%+v: request %+v, status %q, want the refusal %v", tc, request, h.err, want)
		}
		if h.mode != modeConnection {
			t.Fatalf("%+v: a refusal closed the dialog", tc)
		}
	}
}

func TestConnectionDialogEditsInPlace(t *testing.T) {
	h := &fakeConnectionHost{list: []remote.Connection{{Name: "box", Destination: "me@box"}, {Name: "gpu", Destination: "me@gpu"}}}
	var d connectionDialog
	d.open(h, h.list[0])
	if d.name.Value() != "box" || d.destination.Value() != "me@box" {
		t.Fatalf("fields = %q %q, want the edited connection", d.name.Value(), d.destination.Value())
	}
	d.destination.SetValue("me@box2")
	_, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter})
	if request == nil || request.op != connectionUpdate || request.name != "box" || request.next.Destination != "me@box2" {
		t.Fatalf("request = %+v, status %q", request, h.err)
	}
	d.name.SetValue("gpu")
	if _, request := d.handleKey(h, tea.KeyMsg{Type: tea.KeyEnter}); request != nil {
		t.Fatal("renaming onto another connection was accepted")
	}
	if d.view(h); h.title != "⇄ Edit SSH Connection" {
		t.Fatalf("title = %q", h.title)
	}
	d.handleKey(h, tea.KeyMsg{Type: tea.KeyEsc})
	if h.mode != modeList {
		t.Fatalf("esc left mode %v", h.mode)
	}
}

func TestConnectionDialogReopenMovesItsGeneration(t *testing.T) {
	h := &fakeConnectionHost{}
	var d connectionDialog
	d.open(h, remote.Connection{})
	first := d.gen
	d.open(h, remote.Connection{})
	if d.gen == first {
		t.Fatal("a reopened dialog kept the generation a late save would close")
	}
}
