package connections

import (
	"strings"
	"testing"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func typeInto(d *Dialog, text string) {
	for _, r := range text {
		d.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, nil)
	}
}

var plainView = DialogView{
	Field: func(label, value string, _ bool) string { return label + " " + value + "\n" },
	Input: func(in textinput.Model) string { return in.Value() },
	Muted: lipgloss.NewStyle(),
}

func TestDialogAddsAValidConnection(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{})
	typeInto(&d, "box")
	d.Key(tea.KeyMsg{Type: tea.KeyTab}, nil)
	typeInto(&d, "me@box.lan")
	key := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	want := DialogKey{Action: DialogSave, Next: remote.Connection{Name: "box", Destination: "me@box.lan"}, Gen: d.Gen()}
	if key.Action != want.Action || key.Next != want.Next || key.Editing != "" || key.Gen != want.Gen {
		t.Fatalf("enter = %+v, want %+v", key, want)
	}
	if card := d.View(plainView); card.Title != "⇄ New SSH Connection" || !strings.Contains(card.Body, "me@box.lan") {
		t.Fatalf("card = %+v", card)
	}
}

func TestDialogSavesTheBinaryPathOnce(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{Name: "box", Destination: "me@box", Binary: "/opt/am/agent-manager"})
	if d.binary.Value() != "/opt/am/agent-manager" {
		t.Fatalf("edit did not fill the path: %q", d.binary.Value())
	}
	d.binary.SetValue("")
	d.Key(tea.KeyMsg{Type: tea.KeyShiftTab}, nil)
	if d.Focus() != BinaryField {
		t.Fatalf("focus = %v, want the path field last", d.Focus())
	}
	typeInto(&d, "~/bin/am")
	if key := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, nil); key.Action != DialogSave || key.Next.Binary != "~/bin/am" {
		t.Fatalf("enter = %+v", key)
	}
	if again := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, nil); again.Action != DialogRefused || again.Refusal != "this connection is already being saved" {
		t.Fatalf("second enter = %+v", again)
	}
	d.SaveFailed(d.Gen())
	d.binary.SetValue("bin/am")
	if refused := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, nil); refused.Action != DialogRefused || refused.Refusal != "an agent-manager path starts with / or ~/" {
		t.Fatalf("relative path = %+v", refused)
	}
	if d.saving {
		t.Fatal("a refused enter left the dialog saving")
	}
}

// A save that failed for an earlier dialog leaves this one saving.
func TestDialogSaveFailedFencesItsGeneration(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{Name: "box", Destination: "me@box"})
	d.Key(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	d.SaveFailed(d.Gen() - 1)
	if !d.saving {
		t.Fatal("an earlier dialog's failure released this one's save")
	}
}

func TestDialogTypesANameThatSpellsAKey(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{})
	d.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("down"), Paste: true}, nil)
	if d.Focus() != NameField || d.name.Value() != "down" {
		t.Fatalf("focus %v, name %q: a fast-typed name moved the focus", d.Focus(), d.name.Value())
	}
}

func TestDialogRefusesWhatValidationRefuses(t *testing.T) {
	list := []remote.Connection{{Name: "box", Destination: "me@box"}}
	for _, tc := range []struct{ name, destination string }{
		{"", "me@box"},
		{"other", ""},
		{"box", "me@elsewhere"},
		{"other", "-oProxyCommand=sh"},
	} {
		var d Dialog
		d.Open(remote.Connection{})
		d.name.SetValue(tc.name)
		d.destination.SetValue(tc.destination)
		key := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, list)
		want := remote.ValidateConnection(remote.Connection{Name: tc.name, Destination: tc.destination}, list)
		if key.Action != DialogRefused || want == nil || key.Refusal != want.Error() {
			t.Fatalf("%+v: enter %+v, want the refusal %v", tc, key, want)
		}
	}
}

func TestDialogEditsInPlace(t *testing.T) {
	list := []remote.Connection{{Name: "box", Destination: "me@box"}, {Name: "gpu", Destination: "me@gpu"}}
	var d Dialog
	d.Open(list[0])
	if d.name.Value() != "box" || d.destination.Value() != "me@box" {
		t.Fatalf("fields = %q %q, want the edited connection", d.name.Value(), d.destination.Value())
	}
	d.destination.SetValue("me@box2")
	if key := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, list); key.Action != DialogSave || key.Editing != "box" || key.Next.Destination != "me@box2" {
		t.Fatalf("enter = %+v", key)
	}
	d.SaveFailed(d.Gen())
	d.name.SetValue("gpu")
	if key := d.Key(tea.KeyMsg{Type: tea.KeyEnter}, list); key.Action != DialogRefused {
		t.Fatalf("renaming onto another connection = %+v", key)
	}
	if card := d.View(plainView); card.Title != "⇄ Edit SSH Connection" {
		t.Fatalf("title = %q", card.Title)
	}
	if key := d.Key(tea.KeyMsg{Type: tea.KeyEsc}, list); key.Action != DialogCancel {
		t.Fatalf("esc = %+v", key)
	}
}

func TestDialogReopenMovesItsGeneration(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{})
	first := d.Gen()
	d.Open(remote.Connection{})
	if d.Gen() == first {
		t.Fatal("a reopened dialog kept the generation a late save would close")
	}
}

func TestDialogClickFocusesTheFieldOnItsLine(t *testing.T) {
	var d Dialog
	d.Open(remote.Connection{})
	for _, line := range []int{BinaryField, DestinationField, NameField} {
		d.Click(line)
		if d.Focus() != line {
			t.Fatalf("a click on line %d focused %d", line, d.Focus())
		}
	}
	d.Click(fieldCount)
	if d.Focus() != NameField {
		t.Fatal("a click on the note moved the focus")
	}
}
