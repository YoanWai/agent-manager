package config

import (
	"slices"
	"sort"
	"strings"
)

// toolDisplayOrder fixes the order tools appear in when creating a session and
// when cycling the quick-spawn tool. Tools outside this list follow, sorted
// alphabetically.
var toolDisplayOrder = []string{"claude", "opencode", "codex", "grok", "gemini", "pi"}

// AgentToolNames is every configured agent CLI in picker order. The shell
// tool is not a CLI to spawn agents with, so it is left out.
func (c Config) AgentToolNames() []string {
	names := make([]string, 0, len(c.Tools))
	for _, name := range c.ToolNames() {
		if !c.Tools[name].Shell {
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

// EnabledAgentTools is the create-session picker: configured tools minus any
// the user hid in settings. Existing sessions keep their tool even when hidden.
func (c Config) EnabledAgentTools(hidden map[string]bool) []string {
	all := c.AgentToolNames()
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

// DefaultAgentTool is the CLI a new session runs when none is named: the
// settings choice while it is still enabled, else the first enabled one.
func (c Config) DefaultAgentTool(chosen string, hidden map[string]bool) string {
	enabled := c.EnabledAgentTools(hidden)
	if len(enabled) == 0 {
		return ""
	}
	if slices.Contains(enabled, chosen) {
		return chosen
	}
	return enabled[0]
}

// ParseHiddenTools reads the stored list of CLIs hidden from new sessions.
func ParseHiddenTools(raw string) map[string]bool {
	if raw == "" {
		return nil
	}
	hidden := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			hidden[name] = true
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	return hidden
}
