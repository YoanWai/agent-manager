package config

import (
	"errors"
	"maps"
	"runtime"
	"slices"
	"sort"
)

// toolDisplayOrder fixes the order tools appear in when creating a session and
// when cycling the quick-spawn tool. Tools outside this list follow, sorted
// alphabetically.
var toolDisplayOrder = []string{"claude", "opencode", "codex", "grok", "gemini", "pi"}

// CLINames is every configured agent CLI in picker order, leaving out a
// CLI this OS cannot run.
func (c Config) CLINames() []string {
	names := make([]string, 0, len(c.CLIs))
	for _, name := range slices.Sorted(maps.Keys(c.CLIs)) {
		if checkSupported(runtime.GOOS, name) == nil {
			names = append(names, name)
		}
	}
	rank := make(map[string]int, len(toolDisplayOrder))
	for i, name := range toolDisplayOrder {
		rank[name] = i
	}
	sort.Slice(names, func(i, j int) bool {
		ri, iRanked := rank[names[i]]
		rj, jRanked := rank[names[j]]
		if iRanked && jRanked {
			return ri < rj
		}
		if iRanked != jRanked {
			return iRanked
		}
		return names[i] < names[j]
	})
	return names
}

// CheckSupported refuses a tool this OS cannot run.
func CheckSupported(name string) error {
	return checkSupported(runtime.GOOS, name)
}

func checkSupported(goos, name string) error {
	if goos == "windows" && name == "command-code" {
		return errors.New("command-code is not supported on Windows: its binary is named cmd, which Windows resolves to cmd.exe")
	}
	return nil
}

// EnabledCLIs is the create-session picker: configured CLIs minus any the
// user hid in settings. Existing sessions keep their CLI even when hidden.
func (c Config) EnabledCLIs(hidden map[string]bool) []string {
	all := c.CLINames()
	if len(hidden) == 0 {
		return all
	}
	out := make([]string, 0, len(all))
	for _, name := range all {
		if !hidden[name] {
			out = append(out, name)
		}
	}
	return out
}

// DefaultCLI is the CLI a new session runs when none is named: the
// settings choice while it is still enabled, else the first enabled one.
func (c Config) DefaultCLI(chosen string, hidden map[string]bool) string {
	enabled := c.EnabledCLIs(hidden)
	if len(enabled) == 0 {
		return ""
	}
	if slices.Contains(enabled, chosen) {
		return chosen
	}
	return enabled[0]
}
