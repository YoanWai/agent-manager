package review

import (
	"reflect"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// The footer reads the keymap, so it still says what it said when it was
// written out by hand.
func TestLegendReadsTheKeymap(t *testing.T) {
	m := New(false)
	want := []LegendSection{
		{Title: "Review", Pairs: [][2]string{
			{"c", "comment"}, {"d", "remove/handle"}, {"C", "send"}, {"space", "reviewed"},
			{"s", "scope: " + m.scope.String()}, {"r", "repo"}, {"b", "branch"}, {"B", "target"},
		}},
		{Title: "Move", Quiet: true, Pairs: [][2]string{
			{"↑↓/jk", "scroll line"}, {"ctrl+d/ctrl+u", "half page"}, {"pgup/pgdn", "page"},
			{"g/G", "top/bottom"}, {"tab/J K/shift+tab", "file"}, {"n/N", "change"},
			{"u", "layout"}, {"f", "code only"}, {"o/f3", "open file"}, {"?", "keys"},
			{"esc/q", "close"}, {"ctrl+c", "quit"},
		}},
	}
	if got := m.Legend(defaultHelpKey()); !reflect.DeepEqual(got, want) {
		t.Fatalf("legend =\n%v\nwant\n%v", got, want)
	}
}

// Every key a footer pair names runs a binding, and every binding is named
// by some pair or ? row, so the two cannot drift apart.
func TestEveryNamedKeyIsHandled(t *testing.T) {
	named := map[string]bool{}
	m := New(false)
	for _, section := range m.Legend(defaultHelpKey()) {
		for _, pair := range section.Pairs {
			if pair == quitPair {
				continue
			}
			for _, key := range legendKeys(pair[0]) {
				named[key] = true
				if _, ok := bindingFor(key); !ok {
					t.Errorf("footer names %q (%s) but no binding runs it", key, pair[1])
				}
			}
		}
	}
	for _, b := range keymap {
		if result := (&Model{}).run(b.Action, KeyContext{}, 10); b.Action == 0 || !result.Consumed {
			t.Errorf("binding %v names action %d, which run does not handle", b.Keys, b.Action)
		}
		if !named[b.Keys[0]] {
			t.Errorf("binding %v is in no footer pair", b.Keys)
		}
	}
}

// legendKeys reads a footer label back into the key names it stands for.
func legendKeys(label string) []string {
	label = strings.NewReplacer("↑↓", "up/down", "jk", "j/k", " ", "/").Replace(label)
	return strings.Split(label, "/")
}

// A key no binding claims is left to root, unconsumed.
func TestUnboundKeyIsNotConsumed(t *testing.T) {
	m := New(false)
	if result := m.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}, KeyContext{CodeHeight: 10}); result.Consumed {
		t.Fatalf("z was consumed: %+v", result)
	}
	if result := m.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")}, KeyContext{CodeHeight: 10, HelpKey: defaultHelpKey()}); result.Navigation != NavigationHelp {
		t.Fatalf("? = %+v", result)
	}
}

// The ? map's review rows come from the keymap, in its order.
func TestHelpRowsReadTheKeymap(t *testing.T) {
	want := [][2]string{
		{"c", "comment on the line"},
		{"d", "remove a draft, or mark sent feedback handled / open"},
		{"C", "send drafts to the agent as the next review round"},
		{"space", "mark the file reviewed"},
		{"s", "cycle the scope (uncommitted / vs target / last commit / staged)"},
		{"r", "pick the repo when the session dir holds several"},
		{"b", "switch to another worktree, listed by branch"},
		{"B", "pick the target branch the branch diff compares against"},
		{"↑↓ / jk", "move a line"},
		{"ctrl+d / ctrl+u", "half a page"},
		{"pgup / pgdn", "page"},
		{"g / G", "top / bottom of the file"},
		{"tab / shift+tab", "next / previous file (J / K too)"},
		{"n / N", "next / previous change"},
		{"u", "unified / split layout"},
		{"f", "code files only (hides images, assets, lock files)"},
		{"o / f3", "open the current file in your editor"},
		{"?", "this key map"},
		{"esc / q", "close the review"},
	}
	if got := HelpRows(defaultHelpKey()); !reflect.DeepEqual(got, want) {
		t.Fatalf("help rows =\n%v\nwant\n%v", got, want)
	}
}

func defaultHelpKey() keybind.Binding { return keybind.DefaultList().Binding(keybind.Help) }

// The help row follows the configured key, less any key Review already
// answers to, and leaves the footer and the map when nothing is left.
func TestHelpRowNamesTheConfiguredKey(t *testing.T) {
	key, err := keybind.Parse("H")
	if err != nil {
		t.Fatal(err)
	}
	m := New(false)
	for _, tc := range []struct {
		binding keybind.Binding
		want    string
	}{
		{keybind.Keys(key), "H"},
		{keybind.Binding{}, ""},
	} {
		got := ""
		for _, row := range HelpRows(tc.binding) {
			if row[1] == "this key map" {
				got = row[0]
			}
		}
		if got != tc.want {
			t.Fatalf("help row key = %q, want %q", got, tc.want)
		}
		got = ""
		for _, section := range m.Legend(tc.binding) {
			for _, pair := range section.Pairs {
				if pair[1] == "keys" {
					got = pair[0]
				}
			}
		}
		if got != tc.want {
			t.Fatalf("footer key = %q, want %q", got, tc.want)
		}
	}
}
