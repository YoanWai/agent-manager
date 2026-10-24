package persist

import (
	"github.com/YoanWai/agent-manager/internal/config"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// Tools is the configured CLI set the dialog offers, copied from the config.
func Tools(cfg config.Config) uisettings.Tools {
	known := make(map[string]bool, len(cfg.Tools))
	for name := range cfg.Tools {
		known[name] = true
	}
	return uisettings.Tools{Agents: cfg.AgentToolNames(), Known: known}
}

// FromCache builds the dialog the cached values describe, for the palette
// painting the frame now.
func (s State) FromCache(cfg config.Config, theme string) uisettings.State {
	return uisettings.FromSnapshot(uisettings.Snapshot{
		Values:  s.Cache.Values(),
		Hidden:  s.Cache.Hidden(),
		Editors: s.Cache.Editors(),
		Tools:   Tools(cfg),
		Theme:   theme,
	})
}

func (s State) KnownHidden(cfg config.Config) map[string]bool {
	return Tools(cfg).KnownHidden(s.Cache.Hidden())
}

// ToolSelection is the enabled agent CLIs and where the preferred one sits;
// an unset preference falls back to the cached default tool.
func (s State) ToolSelection(cfg config.Config, hidden map[string]bool, preferred string) ([]string, int) {
	names := cfg.EnabledAgentTools(hidden)
	if preferred == "" {
		preferred = s.Cache.Value(uisettings.DefaultToolSetting)
	}
	return names, uisettings.ToolIndex(names, preferred)
}
