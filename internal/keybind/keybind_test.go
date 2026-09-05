package keybind

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestParseSpellsEachSideOfTheKey(t *testing.T) {
	for _, tc := range []struct{ spec, tea, tmux string }{
		{"ctrl+q", "ctrl+q", "C-q"},
		{"Ctrl+Q", "ctrl+q", "C-q"},
		{" ctrl+g ", "ctrl+g", "C-g"},
		{`ctrl+\`, `ctrl+\`, `C-\`},
		{"ctrl+]", "ctrl+]", "C-]"},
		{"ctrl+^", "ctrl+^", "C-^"},
		{"ctrl+_", "ctrl+_", "C-_"},
		{"ctrl+@", "ctrl+@", "C-@"},
		{"alt+g", "alt+g", "M-g"},
		{"Alt+G", "alt+G", "M-G"},
		{"alt+1", "alt+1", "M-1"},
		{"f3", "f3", "F3"},
		{"F12", "f12", "F12"},
		{"q", "q", ""},
		{"K", "K", ""},
		{"shift+k", "K", ""},
		{"?", "?", ""},
		{"|", "|", ""},
		{" ", "space", ""},
		{"space", "space", ""},
		{"Enter", "enter", ""},
		{"shift+up", "shift+up", ""},
		{"shift+tab", "shift+tab", ""},
		{"pgdn", "pgdn", ""},
		{"f", "f", ""},
	} {
		key, err := Parse(tc.spec)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.spec, err)
			continue
		}
		if key.Tea() != tc.tea || key.Tmux() != tc.tmux {
			t.Errorf("Parse(%q) = tea %q tmux %q, want %q / %q", tc.spec, key.Tea(), key.Tmux(), tc.tea, tc.tmux)
		}
	}
}

// esc cancels and ctrl+c quits everywhere, a modifier the terminal folds
// into another key would take that key instead, and the combinations
// Bubble Tea v1 cannot read would never fire.
func TestParseRefusesWhatCannotBeAKey(t *testing.T) {
	for _, tc := range []struct{ spec, reason string }{
		{"esc", "stays as it is"},
		{"ctrl+c", "stays as it is"},
		{"", "a key is empty"},
		{"ctrl+i", "tab"},
		{"ctrl+m", "enter"},
		{"ctrl+[", "escape"},
		{"ctrl+", "one letter"},
		{"ctrl+qq", "one letter"},
		{"ctrl+1", "one letter"},
		{"ctrl+shift+r", "one letter"},
		{"alt+;", "letter or digit"},
		{"alt+", "letter or digit"},
		{"alt+enter", "letter or digit"},
		{"shift+1", "arrow or tab"},
		{"f0", "f1..f12"},
		{"f13", "f1..f12"},
		{"f01", "f1..f12"},
		{"f+1", "f1..f12"},
		{"foo", "f1..f12"},
		{"enterr", "not a key"},
	} {
		_, err := Parse(tc.spec)
		if err == nil {
			t.Errorf("Parse(%q) accepted", tc.spec)
			continue
		}
		if !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("Parse(%q) = %q, want it to say %q", tc.spec, err, tc.reason)
		}
	}
}

func TestGlyphDrawsTheArrowsAndEnter(t *testing.T) {
	for spec, want := range map[string]string{"enter": "↵", "up": "↑", "shift+down": "shift+↓", "backspace": "⌫", "k": "k", "ctrl+r": "ctrl+r"} {
		key, err := Parse(spec)
		if err != nil {
			t.Fatalf("Parse(%q): %v", spec, err)
		}
		if got := key.Glyph(); got != want {
			t.Errorf("Glyph(%q) = %q, want %q", spec, got, want)
		}
	}
	if got := keys("up", "k").Glyph("/"); got != "↑/k" {
		t.Errorf("binding glyph = %q", got)
	}
}

type keysFile struct {
	Session map[string]Binding `toml:"session"`
	List    map[string]Binding `toml:"list"`
}

func decode(t *testing.T, text string) (keysFile, error) {
	t.Helper()
	var file keysFile
	_, err := toml.Decode(text, &file)
	return file, err
}

func TestBindingDecodesOneKeyAListOrNone(t *testing.T) {
	file, err := decode(t, `
[session]
detach = ["f9", "alt+q"]
review = "ctrl+g"
editor = "none"
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	session := file.Session
	if got := session["detach"].Label(); got != "f9 / alt+q" {
		t.Errorf("detach = %q", got)
	}
	if got := session["review"].Label(); got != "ctrl+g" {
		t.Errorf("review = %q", got)
	}
	if editor, written := session["editor"]; !written || editor.Keys() != nil {
		t.Errorf("editor none should be written and empty, got %+v", editor)
	}
	if !session["detach"].Has("alt+q") || session["detach"].Has("ctrl+q") {
		t.Errorf("detach membership wrong: %+v", session["detach"])
	}
}

func TestBindingRefusesBadValues(t *testing.T) {
	for _, tc := range []struct{ text, reason string }{
		{`review = "esc"`, "stays as it is"},
		{`review = ["ctrl+g", "ctrl+g"]`, "listed twice"},
		{`review = [1]`, "written as a string"},
		{`review = 3`, "a key, a list of keys or \"none\""},
		{`review = ["none"]`, "not a key"},
	} {
		_, err := decode(t, "[session]\n"+tc.text)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want %q", tc.text, err, tc.reason)
		}
	}
}

func TestSessionTableFillsOnlyWhatWasLeftOut(t *testing.T) {
	file, err := decode(t, `
[session]
review = "none"
editor = "alt+e"
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	filled, err := SessionTable(file.Session)
	if err != nil {
		t.Fatalf("SessionTable: %v", err)
	}
	if got := filled.Binding(Detach).Label(); got != `ctrl+q / ctrl+\` {
		t.Errorf("detach should take the default, got %q", got)
	}
	if got := filled.Binding(Review).Label(); got != "" {
		t.Errorf("review none should stay off, got %q", got)
	}
	if got := filled.Binding(Editor).Label(); got != "alt+e" {
		t.Errorf("editor should keep its key, got %q", got)
	}
	empty, err := SessionTable(nil)
	if err != nil || !DefaultSession().Equal(empty) {
		t.Errorf("an empty table should be the defaults, got %v", err)
	}
}

func TestSessionTableRefusesASharedKeyANoneDetachAndAPlainKey(t *testing.T) {
	if err := DefaultSession().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	for _, tc := range []struct{ text, reason string }{
		{`review = "ctrl+q"`, "ctrl+q is bound to both detach and review"},
		{`detach = "none"`, "detach needs at least one key"},
		{`editor = "o"`, `keybindings.session.editor: "o" is a plain key, which reaches the agent`},
		{`detach = "enter"`, "plain key"},
		{`revive = "f9"`, `no action named "revive"`},
	} {
		file, err := decode(t, "[session]\n"+tc.text)
		if err != nil {
			t.Fatalf("decode %s: %v", tc.text, err)
		}
		if _, err := SessionTable(file.Session); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want %q", tc.text, err, tc.reason)
		}
	}
}

// The list keeps every key the manager answers to today, so a file that
// names none of them changes nothing.
func TestDefaultListAnswersToTodaysKeys(t *testing.T) {
	list := DefaultList()
	for key, want := range map[string]string{
		"k": Up, "up": Up, "j": Down, "K": ReorderUp, "shift+up": ReorderUp, "enter": Open, "A": Attach,
		"right": StepIn, "left": StepOut, "n": NewSession, "T": Terminal, "g": NewGroup, "f": Fork,
		"space": Prompt, "ctrl+r": Review, ".": MarkIdle, "r": Rename, "m": Move, "o": Editor, "R": Restart,
		"x": Kill, "X": KillAll, "v": Revive, "V": ReviveAll, "a": Archive, "u": Restore, "d": Delete,
		"/": Search, "w": Filter, "t": Archived, "e": EmptyGroups, "F": FoldAll, "|": Resize,
		"s": Settings, "M": Messages, "?": Help, "q": Quit,
	} {
		if got, bound := list.ActionFor(key); !bound || got != want {
			t.Errorf("ActionFor(%q) = %q %v, want %q", key, got, bound, want)
		}
	}
	if _, bound := list.ActionFor("z"); bound {
		t.Error("z is nobody's key")
	}
	if got, _ := list.ActionFor(Normalize("shift+k")); got != ReorderUp {
		t.Errorf("shift+k should normalize onto K, got %q", got)
	}
	if got, _ := list.ActionFor(Normalize(" ")); got != Prompt {
		t.Errorf("the space bar should normalize onto space, got %q", got)
	}
}

func TestListTableMovesAKeyAndRefusesWhatCannotWork(t *testing.T) {
	file, err := decode(t, `
[list]
new_session = "N"
quit = "none"
prompt = ["space", "p"]
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	list, err := ListTable(file.List)
	if err != nil {
		t.Fatalf("ListTable: %v", err)
	}
	if got, _ := list.ActionFor("N"); got != NewSession {
		t.Errorf("N should open a new session, got %q", got)
	}
	if _, bound := list.ActionFor("n"); bound {
		t.Error("n should be free once new_session moved")
	}
	if _, bound := list.ActionFor("q"); bound {
		t.Error("quit none should leave q unbound")
	}
	if got, _ := list.ActionFor("p"); got != Prompt {
		t.Errorf("p should prompt, got %q", got)
	}
	if list.Equal(DefaultList()) || !list.Defaults().Equal(DefaultList()) {
		t.Error("a moved table differs from the defaults, and its Defaults are them")
	}

	for _, tc := range []struct{ text, reason string }{
		{`kill = "n"`, "n is bound to both new_session and kill"},
		{`settings = "none"`, "settings needs at least one key"},
		{`detach = "f9"`, `no action named "detach"`},
	} {
		file, err := decode(t, "[list]\n"+tc.text)
		if err != nil {
			t.Fatalf("decode %s: %v", tc.text, err)
		}
		if _, err := ListTable(file.List); err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want %q", tc.text, err, tc.reason)
		}
	}
}

func TestWithLeavesTheReceiverAlone(t *testing.T) {
	base := DefaultList()
	moved := base.With(Quit, keys("Q"))
	if _, bound := base.ActionFor("Q"); bound {
		t.Error("With changed the table it was called on")
	}
	if got, _ := moved.ActionFor("Q"); got != Quit {
		t.Errorf("moved table should quit on Q, got %q", got)
	}
}
