package launch

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o644)
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
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "[") || !strings.HasSuffix(trim, "]") {
		return "", false
	}
	return strings.TrimSpace(strings.Trim(trim, "[]")), true
}

func tomlKey(line, key string) bool {
	rest, found := strings.CutPrefix(strings.TrimSpace(line), key)
	return found && strings.HasPrefix(strings.TrimSpace(rest), "=")
}
