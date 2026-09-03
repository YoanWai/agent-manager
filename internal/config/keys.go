package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/YoanWai/agent-manager/internal/keybind"
)

const sessionKeysHeader = "[keybindings.session]"

// SaveSessionKeys rewrites the session key table in the config file and
// leaves every other line as it was. The file is the user's: it carries
// their tool blocks and the comments they wrote around them, so the table
// is spliced as text rather than the document being re-encoded.
func SaveSessionKeys(dir string, keys keybind.Session) error {
	if err := keys.Validate(); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := spliceSessionKeys(string(current), keys)
	// A splice is text, so the result is read back before it lands. A file
	// whose keybindings the splice cannot express - one already carrying an
	// inline [keybindings] table - keeps the copy it has.
	var check Config
	if _, err := toml.Decode(updated, &check); err != nil {
		return fmt.Errorf("writing the keys to %s would leave it unreadable: %w", path, err)
	}
	if !sameSessionKeys(check.Keybindings.Session.WithDefaults(), keys) {
		return fmt.Errorf("%s already declares its keybindings another way; edit the file itself", path)
	}
	return atomicfile.WriteFile(path, []byte(updated), 0o644)
}

// spliceSessionKeys replaces the session key table, or appends one when the
// file has none.
func spliceSessionKeys(current string, keys keybind.Session) string {
	block := strings.Split(sessionKeysBlock(keys), "\n")
	lines := strings.Split(current, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == sessionKeysHeader {
			start = i
			break
		}
	}
	if start < 0 {
		body := strings.TrimRight(current, "\n")
		if body == "" {
			return strings.Join(block, "\n") + "\n"
		}
		return body + "\n\n" + strings.Join(block, "\n") + "\n"
	}
	spliced := append([]string{}, lines[:start]...)
	spliced = append(spliced, block...)
	return strings.Join(append(spliced, lines[sessionKeysEnd(lines, start):]...), "\n")
}

// sessionKeysEnd is the line the table stops at: the next table header,
// minus the blank lines and comments written directly above it, which
// introduce that table rather than closing this one.
func sessionKeysEnd(lines []string, start int) int {
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i
			break
		}
	}
	for end-1 > start {
		trimmed := strings.TrimSpace(lines[end-1])
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			break
		}
		end--
	}
	return end
}

func sessionKeysBlock(keys keybind.Session) string {
	return sessionKeysHeader + "\n" +
		"detach = " + bindingValue(keys.Detach) + "\n" +
		"review = " + bindingValue(keys.Review) + "\n" +
		"editor = " + bindingValue(keys.Editor)
}

func bindingValue(binding keybind.Binding) string {
	list := binding.Keys()
	if len(list) == 0 {
		return `"none"`
	}
	quoted := make([]string, 0, len(list))
	for _, key := range list {
		quoted = append(quoted, strconv.Quote(key.Tea()))
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func sameSessionKeys(a, b keybind.Session) bool {
	return a.Detach.Label() == b.Detach.Label() &&
		a.Review.Label() == b.Review.Label() &&
		a.Editor.Label() == b.Editor.Label()
}
