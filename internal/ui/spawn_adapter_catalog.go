package ui

import (
	"maps"
	"slices"
	"time"

	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

// ensureCatalog reads the kept answer or asks the CLI off the update path,
// since an answer takes up to seconds.
func (m *Model) ensureCatalog(toolName string) tea.Cmd {
	cmd, _ := m.ledger.spawner.EnsureCatalog(m.services.cfg, m.services.configDir, toolName, time.Now())
	return cmd
}

func (m *Model) handleCatalog(msg uilaunch.CatalogMsg) tea.Cmd {
	outdated, cmd := m.ledger.spawner.Apply(msg, m.services.cfg, m.services.configDir, time.Now())
	m.fitChoices()
	if outdated != nil && m.catalogToolActive(msg.Tool) {
		m.advanceDialogGen()
		m.presentLaunchHint(uiinstall.Fix{
			Text:    uilaunchhint.OutdatedText(outdatedHint(*outdated), outdatedToolCommand(*outdated)),
			Command: outdatedToolCommand(*outdated),
			Script:  outdatedToolCommand(*outdated),
			Binary:  outdated.Binary,
			Update:  true,
		})
	}
	return cmd
}

// catalogToolActive reports whether the form or the quick bar shows the CLI
// whose answer this is, which is what earns the update dialog.
func (m *Model) catalogToolActive(tool string) bool {
	return (m.form() != nil && m.formTool() == tool) || (m.quick() != nil && m.quickTool() == tool)
}

func (m *Model) fitChoices() {
	if f := m.form(); f != nil {
		f.Choice().Refit(m.spawnHost(), f.Tool())
	}
	if q := m.quick(); q != nil {
		q.Choice().Refit(m.spawnHost(), q.Tool())
	}
}

func (m *Model) saveChoiceSetting(key, value string) {
	m.settings.Cache.ApplyValues([]uisettings.Value{{Key: key, Value: value}})
	m.enqueueEffect(uilaunch.ChoiceSaveRequest{Key: key, Value: value}, 0, false)
}

func (m *Model) choiceSettingKeys() []string {
	return choiceSettingKeys(slices.Sorted(maps.Keys(m.services.cfg.Tools)))
}

// choiceSettingKeys are the per-CLI keys the form and quick prompt read from
// the settings cache.
func choiceSettingKeys(tools []string) []string { return spawn.SettingKeys(tools) }

// refreshFormChoicePrefs and refreshQuickChoicePrefs take a newer read of the
// CLI's kept choice into an open choice.
func (m *Model) refreshFormChoicePrefs() {
	f := m.form()
	f.Choice().RefreshPrefs(m.spawnHost(), f.Tool())
}

func (m *Model) refreshQuickChoicePrefs() {
	q := m.quick()
	q.Choice().RefreshPrefs(m.spawnHost(), q.Tool())
}
