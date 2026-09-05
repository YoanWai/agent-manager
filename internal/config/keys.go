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

// SaveKeys rewrites one key table in the config file and leaves every
// other line as it was. The file is the user's: it carries their tool
// blocks and the comments they wrote around them, so the table is spliced
// as text rather than the document being re-encoded.
func SaveKeys(dir string, keys keybind.Table) error {
	if err := keys.Validate(); err != nil {
		return err
	}
	path := filepath.Join(dir, "config.toml")
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	updated := spliceKeys(string(current), keys)
	// A splice is text, so the result is read back before it lands. A file
	// whose keybindings the splice cannot express - one already carrying an
	// inline [keybindings] table - keeps the copy it has.
	var check Config
	if _, err := toml.Decode(updated, &check); err != nil {
		return fmt.Errorf("writing the keys to %s would leave it unreadable: %w", path, err)
	}
	if err := check.resolveKeys(); err != nil {
		return fmt.Errorf("writing the keys to %s would leave it unreadable: %w", path, err)
	}
	if !check.keys(keys.Scope()).Equal(keys) {
		return fmt.Errorf("%s already declares its keybindings another way; edit the file itself", path)
	}
	return atomicfile.WriteFile(path, []byte(updated), 0o644)
}

func keysHeader(scope string) string {
	return "[keybindings." + scope + "]"
}

func spliceKeys(current string, keys keybind.Table) string {
	header := keysHeader(keys.Scope())
	block := strings.Split(keysBlock(keys), "\n")
	lines := strings.Split(current, "\n")
	start := -1
	for i, line := range lines {
		if isKeysHeader(line, header) {
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
	block[0] = lines[start]
	spliced := append([]string{}, lines[:start]...)
	spliced = append(spliced, block...)
	return strings.Join(append(spliced, lines[keysEnd(lines, start):]...), "\n")
}

func isKeysHeader(line, header string) bool {
	written, _, _ := strings.Cut(line, "#")
	return strings.TrimSpace(written) == header
}

// keysEnd is the line the table stops at: the next table header, minus
// the blank lines and comments written directly above it, which introduce
// that table rather than closing this one.
func keysEnd(lines []string, start int) int {
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

func keysBlock(keys keybind.Table) string {
	lines := []string{keysHeader(keys.Scope())}
	for _, action := range keys.Actions() {
		lines = append(lines, action.Name+" = "+bindingValue(keys.Binding(action.Name)))
	}
	return strings.Join(lines, "\n")
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
