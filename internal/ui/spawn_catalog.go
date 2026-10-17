package ui

import (
	"context"
	"maps"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

type catalogState struct {
	cat       catalog.Catalog
	err       error
	loaded    bool
	loading   bool
	checkedAt time.Time
}

// A running manager rereads the kept answer to catch a CLI upgrade or a
// spawn's refresh, and retries a failure sooner, since a login may be all
// it lacked.
const (
	catalogRecheck = 10 * time.Minute
	catalogRetry   = 30 * time.Second
)

type catalogMsg struct {
	tool string
	cat  catalog.Catalog
	err  error
	// stale is shown while a fresh answer is asked.
	stale bool
}

// ensureCatalog reads the kept answer or asks the CLI off the update path,
// since an answer takes up to seconds.
func (m *Model) ensureCatalog(toolName string) tea.Cmd {
	tool := m.services.cfg.Tools[toolName]
	if tool.Catalog == "" {
		return nil
	}
	if m.ledger.catalogs == nil {
		m.ledger.catalogs = map[string]*catalogState{}
	}
	state := m.ledger.catalogs[toolName]
	if state == nil {
		state = &catalogState{}
		m.ledger.catalogs[toolName] = state
	}
	recheck := catalogRecheck
	if state.err != nil {
		recheck = catalogRetry
	}
	if state.loading || (state.loaded && time.Since(state.checkedAt) < recheck) {
		return nil
	}
	state.loading = true
	configDir := m.services.configDir
	return func() tea.Msg {
		if cat, fresh, ok := catalog.Cached(configDir, toolName, tool); ok {
			return catalogMsg{tool: toolName, cat: cat, stale: !fresh}
		}
		return refreshCatalog(configDir, toolName, tool)
	}
}

func refreshCatalog(configDir, toolName string, tool config.Tool) tea.Msg {
	cat, err := catalog.Refresh(context.Background(), configDir, toolName, tool)
	return catalogMsg{tool: toolName, cat: cat, err: err}
}

func (m *Model) handleCatalog(msg catalogMsg) tea.Cmd {
	state := m.ledger.catalogs[msg.tool]
	if msg.err == nil {
		state.cat = msg.cat
	}
	state.err = msg.err
	state.loaded = true
	state.loading = msg.stale
	state.checkedAt = time.Now()
	m.fitChoices()
	if !msg.stale {
		return nil
	}
	tool, configDir := m.services.cfg.Tools[msg.tool], m.services.configDir
	return func() tea.Msg { return refreshCatalog(configDir, msg.tool, tool) }
}

func (m *Model) fitChoices() {
	if m.mode == modeForm {
		m.form.Choice().Refit(m.spawnHost(), m.form.Tool())
	}
	if m.quick.active {
		m.quick.Choice().Refit(m.spawnHost(), m.quick.Tool())
	}
}

func (m *Model) saveChoiceSetting(key, value string) {
	m.settings.cache.applyValues([]settingValue{{Key: key, Value: value}})
	m.enqueueEffect(choiceSaveRequest{key: key, value: value}, 0, false)
}

func (m *Model) choiceSettingKeys() []string {
	return choiceSettingKeys(slices.Sorted(maps.Keys(m.services.cfg.Tools)))
}

// choiceSettingKeys are the per-CLI keys the form and quick prompt read
// from the settings cache.
func choiceSettingKeys(tools []string) []string { return spawn.SettingKeys(tools) }

// refreshFormChoicePrefs and refreshQuickChoicePrefs take a newer read of
// the CLI's kept choice into an open choice.
func (m *Model) refreshFormChoicePrefs() {
	m.form.Choice().RefreshPrefs(m.spawnHost(), m.form.Tool())
}

func (m *Model) refreshQuickChoicePrefs() {
	m.quick.Choice().RefreshPrefs(m.spawnHost(), m.quick.Tool())
}
