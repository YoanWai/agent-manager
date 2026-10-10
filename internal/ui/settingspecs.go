package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

// SettingSpecs is every key Settings writes and the values each takes, so
// the shell checks a value against the same choices the modal steps
// through.
func SettingSpecs() ([]store.SettingSpec, error) {
	cfg, err := config.Default()
	if err != nil {
		return nil, err
	}
	tools := cfg.AgentToolNames()
	palettes := make([]string, len(themes))
	for i, theme := range themes {
		palettes[i] = theme.Name
	}
	onOff := []string{"on", "off"}
	return []store.SettingSpec{
		{Key: store.DefaultToolSetting, Values: tools, Row: "default tool"},
		{Key: themeSetting, Values: palettes, Default: themes[0].Name, Row: "theme"},
		{Key: themeAutoSetting, Values: onOff, Default: "off", Row: "theme follows OS"},
		{Key: backgroundSetting, Values: []string{"theme", "terminal"}, Default: "theme", Row: "background"},
		{Key: listDensitySetting, Values: []string{"compact", "comfortable"}, Default: "compact", Row: "list density"},
		{Key: sessionLayoutSetting, Values: []string{"split", "full"}, Default: "split", Row: "sessions layout"},
		{Key: hideHeaderSetting, Values: onOff, Default: "off", Row: "header"},
		{Key: hideStatsSetting, Values: onOff, Default: "off", Row: "computer stats"},
		{Key: diffLayoutSetting, Values: []string{"split", "unified"}, Default: "split", Row: "review layout"},
		{Key: quickCloseSetting, Values: []string{"stay", "close"}, Default: "stay", Row: "after quick prompt"},
		{Key: focusKeySetting, Values: []string{"focus", "attach"}, Default: "focus", Row: "session keys"},
		{Key: arrowStepSetting, Values: onOff, Default: "on", Row: "←→ step in/out"},
		{Key: mouseSetting, Values: onOff, Default: "on", Row: "mouse"},
		{Key: worktreeSetting, Values: onOff, Default: "off", Row: "spawn in worktree"},
		{Key: baseFetchSetting, Values: onOff, Default: "on", Row: "fetch on spawn"},
		{Key: store.CoordinationSetting, Values: []string{"on-request", "proactive"}, Default: "on-request", Row: "coordination"},
		{Key: notificationsSetting, Values: onOff, Default: "on", Row: "notifications"},
		{Key: notifyFinishedSetting, Values: onOff, Default: "off", Row: "notify on finish"},
		{Key: store.EditorSetting, Row: "editor"},
		{Key: store.HiddenToolsSetting, Values: tools, List: true, Row: "CLIs"},
	}, nil
}
