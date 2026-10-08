package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

const diffLayoutSetting = "diff_layout"

// sessionLayoutSetting is the list's shape: "full" gives the rail the whole
// width, anything else keeps the split with the preview beside it.
const sessionLayoutSetting = "layout"

const listDensitySetting = "list_density"

const hideHeaderSetting = "hide_header"

const hideStatsSetting = "hide_stats"

// backgroundSetting is what fills the backdrop's cells: "terminal" leaves
// the terminal's own colors, anything else paints the theme's.
const backgroundSetting = "background"

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

// baseFetchSetting is the fetch ahead of a worktree spawn: "off" skips it,
// anything else fetches (the default).
const baseFetchSetting = "worktree_fetch"

const notificationsSetting = "notifications"

const notifyFinishedSetting = "notify_finished"

// hiddenToolsSetting lists CLI tools omitted from new-session pickers
// (comma-separated names). Empty means every configured tool is shown.
const hiddenToolsSetting = "hidden_tools"

// editorSetting is the row store.Editor reads; the settings lane writes it
// with the other preferences.
const editorSetting = "editor"

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
	baseFetch       bool
	proactive       bool
	notifications   bool
	notifyFinished  bool
	themeAuto       bool
	// terminalBackground is the background row's choice, applied to the
	// model as it is stepped so the frame previews it.
	terminalBackground bool
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
	editor     editorRow
	dirty      bool
}

type settingsCache struct {
	values map[string]string
	hidden map[string]bool
	// editors is the last PATH probe, kept so a dialog rebuilt from a
	// later load still lists what was found.
	editors *editorsProbedMsg
}

func (c settingsCache) value(key string) string {
	return c.values[key]
}

const (
	settingsFieldTool = iota
	settingsFieldTheme
	settingsFieldThemeAuto
	settingsFieldBackground
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
	settingsFieldBaseFetch
	settingsFieldCoordination
	settingsFieldNotify
	settingsFieldNotifyFinish
	settingsFieldEditor
	settingsFieldKeybindings
	settingsFieldCLIs
	settingsFieldBugReport
	settingsFieldFeatureRequest
	settingsFieldUpdate
	settingsFieldCount
)

// settingsHost is what the Settings dialog's keys reach in the root: the
// status bar, the configured tools, the live key tables, the effect lane
// its captured writes go through, and the two live previews a step makes.
// Closing the dialog, the in-place update, and the store writes stay with
// the root adapters.
type settingsHost interface {
	reportErr(text string)
	clearErr()
	toolConfig() config.Config
	keyTables() (session, list keybind.Table)
	queuedEffects() []effectRequest
	submitEffect(request effectRequest) tea.Cmd
	syncPaneTheme() tea.Cmd
	previewBackground(terminal bool)
}

// settingsViewHost is what painting the dialog reads from the root: the
// live key tables for the keybindings row, the release facts for the
// version row, the room the key picker scrolls in, and the card chrome.
type settingsViewHost interface {
	keyTables() (session, list keybind.Table)
	release() (version, latest string, applying bool)
	dialogHeight() int
	card(title, body string, hint [][2]string) string
	cardFlex(title, body string, hint [][2]string) string
	confirmCard(title, question, consequence string, destructive bool, answer string) string
}

// settingsExit is what a dialog key asks of the root beyond the dialog's
// own state.
type settingsExit uint8

const (
	settingsStay settingsExit = iota
	settingsSave
	settingsUpdate
	settingsReportBug
)
