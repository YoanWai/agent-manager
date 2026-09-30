package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
)

const diffLayoutSetting = "diff_layout"

// sessionLayoutSetting is the list's shape: "full" gives the rail the whole
// width, anything else keeps the split with the preview beside it.
const sessionLayoutSetting = "layout"

const listDensitySetting = "list_density"

const hideHeaderSetting = "hide_header"

const hideStatsSetting = "hide_stats"

const focusKeySetting = "focus_key"

// arrowStepSetting is the beta ←→ pair: "off" turns it off, anything else
// leaves it on.
const arrowStepSetting = "arrow_step_keys"

// mouseSetting is app-wide mouse reporting: "off" gives the rail and
// content column back to the terminal's own click-drag text selection,
// anything else leaves it on (the default).
const mouseSetting = "mouse_mode"

const quickCloseSetting = "quick_prompt_close"

const worktreeSetting = "worktree_default"

const notificationsSetting = "notifications"

const notifyFinishedSetting = "notify_finished"

// hiddenToolsSetting lists CLI tools omitted from new-session pickers
// (comma-separated names). Empty means every configured tool is shown.
const hiddenToolsSetting = "hidden_tools"

type settingsState struct {
	toolNames       []string
	toolIndex       int
	themeIndex      int
	field           int
	layoutSplit     bool
	quickCloseSend  bool
	enterFocuses    bool
	arrowStep       bool
	comfortableRows bool
	fullLayout      bool
	hideHeader      bool
	hideStats       bool
	mouseDisabled   bool
	worktreeDefault bool
	proactive       bool
	notifications   bool
	notifyFinished  bool
	themeAuto       bool
	// manualTheme is the persisted choice the theme key keeps while
	// auto-detect drives the live palette, so turning auto off returns
	// to it.
	manualTheme string
	// cliPicker is the sub-panel for which CLIs appear when creating sessions.
	cliPicker  bool
	cliNames   []string
	cliHidden  map[string]bool
	cliCursor  int
	keyPicker  bool
	tables     []keybind.Table
	keyCursor  int
	keyCapture bool
	keyAppend  bool
	keyReset   bool
}

const (
	settingsFieldTool = iota
	settingsFieldTheme
	settingsFieldThemeAuto
	settingsFieldDensity
	settingsFieldSessionLayout
	settingsFieldHeader
	settingsFieldStats
	settingsFieldLayout
	settingsFieldQuickClose
	settingsFieldFocusKey
	settingsFieldArrowStep
	settingsFieldMouse
	settingsFieldWorktree
	settingsFieldCoordination
	settingsFieldNotify
	settingsFieldNotifyFinish
	settingsFieldKeybindings
	settingsFieldCLIs
	settingsFieldBugReport
	settingsFieldFeatureRequest
	settingsFieldUpdate
	settingsFieldCount
)
