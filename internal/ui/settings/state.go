// Package settings is the Settings dialog: its rows, the CLI and key
// pickers, the editor row, and what each step means for the values the
// store keeps. It receives copied values and returns typed results; the root
// adapter runs the store writes, the palette repaint and the links.
package settings

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// The keys Settings writes.
const (
	GlyphModeSetting   = presentation.GlyphModeSetting
	DefaultToolSetting = "default_tool"
	ThemeSetting       = "theme"
	ThemeAutoSetting   = "theme_auto"
	DiffLayoutSetting  = "diff_layout"
	// SessionLayoutSetting is the list's shape: "full" gives the rail the
	// whole width, anything else keeps the split with the preview beside it.
	SessionLayoutSetting = "layout"
	ListDensitySetting   = "list_density"
	HideHeaderSetting    = "hide_header"
	HideStatsSetting     = "hide_stats"
	// BackgroundSetting is what fills the backdrop's cells: "terminal"
	// leaves the terminal's own colors, anything else paints the theme's.
	BackgroundSetting = "background"
	FocusKeySetting   = "focus_key"
	// ArrowStepSetting is the beta ←→ pair: "off" turns it off, anything
	// else leaves it on.
	ArrowStepSetting = "arrow_step_keys"
	// MouseSetting is app-wide mouse reporting: "off" gives the rail and
	// content column back to the terminal's own click-drag text selection,
	// anything else leaves it on (the default).
	MouseSetting      = "mouse_mode"
	QuickCloseSetting = "quick_prompt_close"
	WorktreeSetting   = "worktree_default"
	// BaseFetchSetting is the fetch ahead of a worktree spawn: "off" skips
	// it, anything else fetches (the default).
	BaseFetchSetting      = "worktree_fetch"
	CoordinationSetting   = "coordination"
	NotificationsSetting  = "notifications"
	NotifyFinishedSetting = "notify_finished"
	// NotifyCommandSetting is a shell command run on every notification,
	// with the event in AM_* variables. Empty runs nothing.
	NotifyCommandSetting = "notify_command"
	// HiddenToolsSetting lists CLI tools omitted from new-session pickers
	// (comma-separated names). Empty means every configured tool is shown.
	HiddenToolsSetting = "hidden_tools"
	// EditorSetting is the row store.Editor reads; the settings lane writes
	// it with the other preferences.
	EditorSetting = "editor"
)

const (
	FieldTool = iota
	FieldTheme
	FieldThemeAuto
	FieldBackground
	FieldGlyphs
	FieldDensity
	FieldSessionLayout
	FieldHeader
	FieldStats
	FieldLayout
	FieldQuickClose
	FieldFocusKey
	FieldArrowStep
	FieldMouse
	FieldWorktree
	FieldBaseFetch
	FieldCoordination
	FieldNotify
	FieldNotifyFinish
	FieldEditor
	FieldKeybindings
	FieldCLIs
	FieldDocs
	FieldBugReport
	FieldFeatureRequest
	FieldUpdate
	FieldCount
)

// State is the open dialog: the choice on every row, the sub-pickers, and
// the baseline a save compares against.
type State struct {
	GlyphMode                                               presentation.GlyphMode
	DocsRow, GlyphRow, GlyphRowWidth, SaveRow, SaveRowWidth int
	ToolNames                                               []string
	ToolIndex                                               int
	ThemeIndex                                              int
	Field                                                   int
	LayoutSplit                                             bool
	QuickCloseSend                                          bool
	EnterFocuses                                            bool
	ArrowStep                                               bool
	ComfortableRows                                         bool
	FullLayout                                              bool
	HideHeader                                              bool
	HideStats                                               bool
	MouseDisabled                                           bool
	WorktreeDefault                                         bool
	BaseFetch                                               bool
	Proactive                                               bool
	Notifications                                           bool
	NotifyFinished                                          bool
	ThemeAuto                                               bool
	// TerminalBackground is the background row's choice, applied to the
	// model as it is stepped so the frame previews it.
	TerminalBackground bool
	// ManualTheme is the persisted choice the theme key keeps while
	// auto-detect drives the live palette, so turning auto off returns
	// to it.
	ManualTheme string
	// CLIPicker is the sub-panel for which CLIs appear when creating sessions.
	CLIPicker  bool
	CLINames   []string
	CLIHidden  map[string]bool
	CLICursor  int
	KeyPicker  bool
	Tables     []keybind.Table
	KeyCursor  int
	KeyCapture bool
	KeyAppend  bool
	KeyReset   bool
	Editor     EditorRow
	Dirty      bool
	// Baseline is what the dialog started from, by key: a save writes
	// only keys that differ, so it never puts this process's stale cache
	// over another writer's value. A missing key is always written.
	Baseline       map[string]string
	BaselineHidden *string
}

// Tools is the configured CLI set the dialog offers, copied from the config.
type Tools struct {
	// Agents is every agent CLI in picker order.
	Agents []string
	// Known holds every configured tool name, shells included.
	Known map[string]bool
}

// Enabled is Agents without the hidden ones.
func (t Tools) Enabled(hidden map[string]bool) []string {
	if len(hidden) == 0 {
		return t.Agents
	}
	out := make([]string, 0, len(t.Agents))
	for _, name := range t.Agents {
		if !hidden[name] {
			out = append(out, name)
		}
	}
	return out
}

// KnownHidden keeps the cached hidden names that are still configured tools.
func (t Tools) KnownHidden(cached map[string]bool) map[string]bool {
	hidden := make(map[string]bool)
	for name, on := range cached {
		if on && t.Known[name] {
			hidden[name] = true
		}
	}
	return hidden
}

// ToolIndex is where preferred sits among names, or the first entry.
func ToolIndex(names []string, preferred string) int {
	for index, name := range names {
		if name == preferred {
			return index
		}
	}
	return 0
}

// Snapshot is what opening the dialog copies from the root: the cached
// store values, the cached hidden CLIs, the last editor probe, the tools,
// and the palette painting the frame now.
type Snapshot struct {
	Values  map[string]string
	Hidden  map[string]bool
	Editors *EditorsProbe
	Tools   Tools
	Theme   string
}

// FromSnapshot builds the dialog the cached values describe.
func FromSnapshot(s Snapshot) State {
	value := func(key string) string { return s.Values[key] }
	hidden := s.Tools.KnownHidden(s.Hidden)
	names := s.Tools.Enabled(hidden)
	state := State{
		GlyphMode:   presentation.GlyphMode(value(GlyphModeSetting)),
		ToolNames:   names,
		ToolIndex:   ToolIndex(names, value(DefaultToolSetting)),
		ThemeIndex:  uitheme.Index(s.Theme),
		ManualTheme: uitheme.Palettes[uitheme.Index(value(ThemeSetting))].Name,
		CLIHidden:   hidden,
		Editor:      NewEditorRow(value(EditorSetting), s.Editors),
	}
	for _, setting := range table {
		if setting.Flag != nil {
			*setting.Flag(&state) = setting.on(value(setting.Key))
		}
	}
	return state
}

// Busy reports a dialog a fresh store read must not rebuild: one with
// unsaved steps or a sub-picker open.
func (s State) Busy() bool {
	return s.Dirty || s.CLIPicker || s.KeyPicker
}

// InSubpanel reports a sub-picker or a typed line covering the rows.
func (s State) InSubpanel() bool {
	return s.CLIPicker || s.KeyPicker || s.Editor.Typing()
}
