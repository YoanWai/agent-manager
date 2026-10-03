package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
)

func writeLegacyFile(t *testing.T, text string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(text), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return dir
}

// The tool blocks and the poll interval an older file carries are not read,
// so a hand-edit that no longer fits them cannot fail the import.
func TestReadLegacyFileTakesTheEditorAndTheNamedKeys(t *testing.T) {
	dir := writeLegacyFile(t, `
poll_interval = 3
editor = "code -n"

[tools.claude]
shell = "yes"
rules = "not an array"

[keybindings.session]
detach = ["f9", "alt+q"]
review = "none"

[keybindings.list]
new_session = "N"
`)
	file, found, err := ReadLegacyFile(dir)
	if err != nil || !found {
		t.Fatalf("ReadLegacyFile = found %v, %v", found, err)
	}
	if file.Editor != "code -n" {
		t.Fatalf("editor = %q", file.Editor)
	}
	if got := file.Keybindings.Session[keybind.Detach].Label(); got != "f9 / alt+q" {
		t.Errorf("detach = %q", got)
	}
	if review, named := file.Keybindings.Session[keybind.Review]; !named || review.Label() != "" {
		t.Errorf("review none should be named and off, got %q named %v", review.Label(), named)
	}
	if _, named := file.Keybindings.Session[keybind.Editor]; named {
		t.Error("an action the file left out should stay unnamed")
	}
	if got := file.Keybindings.List[keybind.NewSession].Label(); got != "N" {
		t.Errorf("new_session = %q", got)
	}
}

func TestReadLegacyFileFindsNothingAndWritesNothing(t *testing.T) {
	dir := t.TempDir()
	_, found, err := ReadLegacyFile(dir)
	if err != nil || found {
		t.Fatalf("ReadLegacyFile = found %v, %v", found, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("reading must not create the file: %v", err)
	}
}

func TestReadLegacyFileRefusesAKeyTableThatCannotWork(t *testing.T) {
	for _, tc := range []struct{ table, text, reason string }{
		{"session", `editor = "ctrl+i"`, "ctrl+i is tab"},
		{"session", `editor = "o"`, `"o" is a plain key, which reaches the agent`},
		{"session", `detach = "none"`, "detach needs at least one key"},
		{"list", `settings = "none"`, "settings needs at least one key"},
		{"list", `quit = "esc"`, "stays as it is"},
		{"list", `detach = "f9"`, `no action named "detach"`},
	} {
		dir := writeLegacyFile(t, "[keybindings."+tc.table+"]\n"+tc.text+"\n")
		_, _, err := ReadLegacyFile(dir)
		if err == nil || !strings.Contains(err.Error(), tc.reason) {
			t.Errorf("%s %s: err = %v, want %q", tc.table, tc.text, err, tc.reason)
		}
	}
}

func TestReadLegacyFileNamesAFileItCannotParse(t *testing.T) {
	dir := writeLegacyFile(t, "editor = \n")
	_, _, err := ReadLegacyFile(dir)
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "config.toml")) {
		t.Fatalf("err = %v, want the path named", err)
	}
}
