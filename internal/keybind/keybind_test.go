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

// A plain key would take a character away from the agent, a modifier the
// terminal folds into another key would take that key instead, and the
// combinations Bubble Tea v1 cannot read would never fire.
func TestParseRefusesWhatCannotBeASessionKey(t *testing.T) {
	for _, tc := range []struct{ spec, reason string }{
		{"q", "plain key reaches the agent"},
		{"enter", "plain key reaches the agent"},
		{"esc", "plain key reaches the agent"},
		{"", "plain key reaches the agent"},
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
		{"f0", "f1..f12"},
		{"f13", "f1..f12"},
		{"f01", "f1..f12"},
		{"f+1", "f1..f12"},
		{"foo", "f1..f12"},
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

type sessionFile struct {
	Session Session `toml:"session"`
}

func decode(t *testing.T, text string) (Session, error) {
	t.Helper()
	var file sessionFile
	_, err := toml.Decode(text, &file)
	return file.Session, err
}

func TestBindingDecodesOneKeyAListOrNone(t *testing.T) {
	session, err := decode(t, `
[session]
detach = ["f9", "alt+q"]
review = "ctrl+g"
editor = "none"
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got := session.Detach.Label(); got != "f9 / alt+q" {
		t.Errorf("detach = %q", got)
	}
	if got := session.Review.Label(); got != "ctrl+g" {
		t.Errorf("review = %q", got)
	}
	if session.Editor.Keys() != nil || !session.Editor.set {
		t.Errorf("editor none should be set and empty, got %+v", session.Editor)
	}
	if !session.Detach.Has("alt+q") || session.Detach.Has("ctrl+q") {
		t.Errorf("detach membership wrong: %+v", session.Detach)
	}
}

func TestBindingRefusesBadValues(t *testing.T) {
	for _, tc := range []struct{ text, reason string }{
		{`review = "q"`, "plain key"},
		{`review = ["ctrl+g", "ctrl+g"]`, "listed twice"},
		{`review = [1]`, "written as a string"},
		{`review = 3`, "a key, a list of keys or \"none\""},
		{`review = ["none"]`, "plain key"},
	} {
		_, err := decode(t, "[session]\n"+tc.text)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s: err = %v, want %q", tc.text, err, tc.reason)
		}
	}
}

func TestWithDefaultsFillsOnlyWhatWasLeftOut(t *testing.T) {
	session, err := decode(t, `
[session]
review = "none"
editor = "alt+e"
`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	filled := session.WithDefaults()
	if got := filled.Detach.Label(); got != `ctrl+q / ctrl+\` {
		t.Errorf("detach should take the default, got %q", got)
	}
	if got := filled.Review.Label(); got != "" {
		t.Errorf("review none should stay off, got %q", got)
	}
	if got := filled.Editor.Label(); got != "alt+e" {
		t.Errorf("editor should keep its key, got %q", got)
	}
	if got := (Session{}).WithDefaults(); got.Detach.Label() != `ctrl+q / ctrl+\` ||
		got.Review.Label() != "ctrl+r" || got.Editor.Label() != "f3" {
		t.Errorf("empty table should be the defaults, got %+v", got)
	}
}

func TestValidateRefusesASharedKeyAndANoneDetach(t *testing.T) {
	if err := DefaultSession().Validate(); err != nil {
		t.Fatalf("defaults: %v", err)
	}
	shared, err := decode(t, "[session]\nreview = \"ctrl+q\"")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := shared.WithDefaults().Validate(); err == nil || !strings.Contains(err.Error(), "ctrl+q is bound to both detach and review") {
		t.Errorf("shared key: %v", err)
	}
	stranded, err := decode(t, "[session]\ndetach = \"none\"")
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := stranded.WithDefaults().Validate(); err == nil || !strings.Contains(err.Error(), "detach needs at least one key") {
		t.Errorf("detach none: %v", err)
	}
}
