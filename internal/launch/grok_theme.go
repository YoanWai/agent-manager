package launch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
)

func ensureGrokTerminalTheme() error {
	dir := os.Getenv("GROK_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		dir = filepath.Join(home, ".grok")
	}
	return ensureGrokTerminalThemeFile(filepath.Join(dir, "config.toml"))
}

func ensureGrokTerminalThemeFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	updated := setTomlKey(string(data), "ui", "theme", `"terminal"`)
	updated = setTomlKey(updated, "features", "terminal_theme", "true")
	if updated == string(data) {
		return nil
	}
	if err := checkOnlyThemeChanged(string(data), updated); err != nil {
		return fmt.Errorf(`cannot set Grok's terminal theme in %s, set theme = "terminal" under [ui] and terminal_theme = true under [features] by hand: %w`, path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

// setTomlKey edits lines, so the result must decode to the old config plus the two keys and nothing else.
func checkOnlyThemeChanged(before, after string) error {
	want := map[string]any{}
	if _, err := toml.Decode(before, &want); err != nil {
		return err
	}
	got := map[string]any{}
	if _, err := toml.Decode(after, &got); err != nil {
		return err
	}
	setTomlValue(want, "ui", "theme", "terminal")
	setTomlValue(want, "features", "terminal_theme", true)
	if !reflect.DeepEqual(want, got) {
		return errors.New("the file declares them in a form this edit would change")
	}
	return nil
}

func setTomlValue(doc map[string]any, table, key string, value any) {
	section, ok := doc[table].(map[string]any)
	if !ok {
		section = map[string]any{}
		doc[table] = section
	}
	section[key] = value
}

func setTomlKey(text, section, key, value string) string {
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	var out []string
	current := ""
	seen := false
	wrote := false
	flush := func() {
		if current != section || seen {
			return
		}
		at := len(out)
		for at > 0 && out[at-1] == "" {
			at--
		}
		out = slices.Insert(out, at, key+" = "+value)
		wrote = true
	}
	for _, line := range lines {
		if name, ok := tomlSection(line); ok {
			flush()
			current = name
			seen = false
		} else if current == section && tomlKey(line, key) {
			line = key + " = " + value
			seen = true
			wrote = true
		}
		out = append(out, line)
	}
	flush()
	if !wrote {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
		out = append(out, "["+section+"]", key+" = "+value, "")
	}
	return strings.Join(out, "\n")
}

func tomlSection(line string) (string, bool) {
	header, _, _ := strings.Cut(line, "#")
	trim := strings.TrimSpace(header)
	if !strings.HasPrefix(trim, "[") || !strings.HasSuffix(trim, "]") {
		return "", false
	}
	name := strings.TrimSpace(strings.Trim(trim, "[]"))
	return strings.Trim(name, `"'`), true
}

func tomlKey(line, key string) bool {
	trim := strings.TrimSpace(line)
	for _, spelling := range []string{key, `"` + key + `"`, "'" + key + "'"} {
		if rest, found := strings.CutPrefix(trim, spelling); found && strings.HasPrefix(strings.TrimSpace(rest), "=") {
			return true
		}
	}
	return false
}
