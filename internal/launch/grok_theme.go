package launch

import (
	"os"
	"path/filepath"
	"strings"
)

func ensureGrokTerminalTheme() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return ensureGrokTerminalThemeFile(filepath.Join(home, ".grok", "config.toml"))
}

func ensureGrokTerminalThemeFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		body := "[ui]\ntheme = \"terminal\"\n\n[features]\nterminal_theme = true\n"
		return os.WriteFile(path, []byte(body), 0o644)
	}
	updated := setTomlKey(string(data), "ui", "theme", `"terminal"`)
	updated = setTomlKey(updated, "features", "terminal_theme", "true")
	if updated == string(data) {
		return nil
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func setTomlKey(text, section, key, value string) string {
	lines := strings.Split(text, "\n")
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
		out = append(out, "")
		copy(out[at+1:], out[at:])
		out[at] = key + " = " + value
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
	if !strings.HasPrefix(trim, "[") || !strings.HasSuffix(trim, "]") || strings.HasPrefix(trim, "[[") {
		return "", false
	}
	name := strings.TrimSpace(trim[1 : len(trim)-1])
	if name == "" || strings.Contains(name, "[") {
		return "", false
	}
	return name, true
}

func tomlKey(line, key string) bool {
	trim := strings.TrimSpace(line)
	if strings.HasPrefix(trim, "#") || !strings.HasPrefix(trim, key) {
		return false
	}
	rest := strings.TrimSpace(trim[len(key):])
	return strings.HasPrefix(rest, "=")
}
