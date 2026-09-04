package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func keysFrom(t *testing.T, detach, review, editor []string) keybind.Session {
	t.Helper()
	binding := func(specs []string) keybind.Binding {
		keys := make([]keybind.Key, 0, len(specs))
		for _, spec := range specs {
			key, err := keybind.Parse(spec)
			if err != nil {
				t.Fatalf("Parse(%q): %v", spec, err)
			}
			keys = append(keys, key)
		}
		return keybind.Keys(keys...)
	}
	return keybind.Session{Detach: binding(detach), Review: binding(review), Editor: binding(editor)}
}

func writeConfig(t *testing.T, text string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir, path
}

func readConfig(t *testing.T, path string) string {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	return string(text)
}

// The file belongs to the user: their tool blocks and the comments around
// them survive a table the settings screen writes.
func TestSaveSessionKeysAppendsAndKeepsTheRestOfTheFile(t *testing.T) {
	original := `poll_interval = "2s"

# the CLI I actually use
[tools.claude]
command = "claude"
`
	dir, path := writeConfig(t, original)
	keys := keysFrom(t, []string{"ctrl+q", `ctrl+\`}, []string{"alt+r"}, nil)
	if err := SaveSessionKeys(dir, keys); err != nil {
		t.Fatalf("SaveSessionKeys: %v", err)
	}

	saved := readConfig(t, path)
	for _, line := range strings.Split(strings.TrimRight(original, "\n"), "\n") {
		if !strings.Contains(saved, line) {
			t.Fatalf("saving dropped %q:\n%s", line, saved)
		}
	}
	for _, want := range []string{
		"[keybindings.session]",
		`detach = ["ctrl+q", "ctrl+\\"]`,
		`review = "alt+r"`,
		`editor = "none"`,
	} {
		if !strings.Contains(saved, want) {
			t.Fatalf("saved file is missing %q:\n%s", want, saved)
		}
	}

	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if !loaded.Keybindings.Session.Equal(keys) {
		t.Fatalf("reloaded keys = %q / %q / %q", loaded.Keybindings.Session.Detach.Label(),
			loaded.Keybindings.Session.Review.Label(), loaded.Keybindings.Session.Editor.Label())
	}
}

// A second save replaces the table it wrote rather than stacking another
// one, and the table after it keeps the comment introducing it.
func TestSaveSessionKeysReplacesTheTableInPlace(t *testing.T) {
	dir, path := writeConfig(t, `[keybindings.session]
detach = "f9"
review = "none"
editor = "none"

# my own tool
[tools.mine]
command = "mine"
`)
	if err := SaveSessionKeys(dir, keysFrom(t, []string{"ctrl+q"}, []string{"ctrl+r"}, []string{"f3"})); err != nil {
		t.Fatalf("SaveSessionKeys: %v", err)
	}

	saved := readConfig(t, path)
	if strings.Count(saved, "[keybindings.session]") != 1 {
		t.Fatalf("the table should be replaced, not repeated:\n%s", saved)
	}
	if strings.Contains(saved, `detach = "f9"`) {
		t.Fatalf("the old keys should be gone:\n%s", saved)
	}
	if !strings.Contains(saved, "# my own tool\n[tools.mine]") {
		t.Fatalf("the next table lost the comment above it:\n%s", saved)
	}
	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := loaded.Keybindings.Session.Detach.Label(); got != "ctrl+q" {
		t.Fatalf("reloaded detach = %q", got)
	}
	if got := loaded.Tools["mine"].Command; got != "mine" {
		t.Fatalf("the tool block should survive, command = %q", got)
	}
}

func TestSaveSessionKeysKeepsAnInlineCommentOnTheHeader(t *testing.T) {
	dir, path := writeConfig(t, `[keybindings.session] # session shortcuts
detach = "f9"
review = "none"
editor = "none"
`)
	if err := SaveSessionKeys(dir, keysFrom(t, []string{"ctrl+q"}, []string{"ctrl+r"}, []string{"f3"})); err != nil {
		t.Fatalf("SaveSessionKeys: %v", err)
	}

	saved := readConfig(t, path)
	if strings.Count(saved, "[keybindings.session]") != 1 {
		t.Fatalf("the table should be replaced, not repeated:\n%s", saved)
	}
	if !strings.Contains(saved, "[keybindings.session] # session shortcuts\n") {
		t.Fatalf("the header lost its comment:\n%s", saved)
	}
	loaded, err := LoadDir(dir)
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if got := loaded.Keybindings.Session.Detach.Label(); got != "ctrl+q" {
		t.Fatalf("reloaded detach = %q", got)
	}
}

// A table the splice cannot express leaves the file alone rather than
// writing something that would not load.
func TestSaveSessionKeysRefusesAFileItWouldBreak(t *testing.T) {
	original := "[keybindings]\nsession = { detach = \"ctrl+q\" }\n"
	dir, path := writeConfig(t, original)
	err := SaveSessionKeys(dir, keysFrom(t, []string{"f9"}, nil, nil))
	if err == nil {
		t.Fatal("saving over an inline keybindings table should fail")
	}
	if !strings.Contains(err.Error(), "another way") && !strings.Contains(err.Error(), "unreadable") {
		t.Fatalf("error should name the conflict, got %v", err)
	}
	if got := readConfig(t, path); got != original {
		t.Fatalf("the file should be untouched, got:\n%s", got)
	}
}

func TestSaveSessionKeysRefusesATableWithNoWayBack(t *testing.T) {
	original := "poll_interval = \"2s\"\n"
	dir, path := writeConfig(t, original)
	err := SaveSessionKeys(dir, keysFrom(t, nil, []string{"ctrl+r"}, []string{"f3"}))
	if err == nil || !strings.Contains(err.Error(), "detach needs at least one key") {
		t.Fatalf("err = %v, want the detach rule", err)
	}
	if got := readConfig(t, path); got != original {
		t.Fatalf("the file should be untouched, got:\n%s", got)
	}
}
