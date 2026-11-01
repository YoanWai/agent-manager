package persist

import (
	"github.com/YoanWai/agent-manager/internal/config"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

// Tools is the configured CLI set the dialog offers, copied from the config.
func Tools(cfg config.Config) uisettings.Tools {
	known := make(map[string]bool, len(cfg.CLIs))
	for name := range cfg.CLIs {
		known[name] = true
	}
	return uisettings.Tools{Agents: cfg.CLINames(), Known: known}
}

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
// an unset preference falls back to the cached default CLI.
func (s State) ToolSelection(cfg config.Config, hidden map[string]bool, preferred string) ([]string, int) {
	names := cfg.EnabledCLIs(hidden)
	if preferred == "" {
		preferred = s.Cache.Value(uisettings.DefaultToolSetting)
	}
	return names, uisettings.ToolIndex(names, preferred)
}
