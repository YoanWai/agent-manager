package settings

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// Setting describes one key once: the spec the shell checks a value
// against, the dialog row that steps it, and for a two-way row the State
// flag it keeps with the stored spelling of each side.
type Setting struct {
	Spec
	Field int
	// Flag is the State bool a two-way row steps; nil for a row with its
	// own stepping (the tool, theme, symbols and editor rows).
	Flag func(*State) *bool
	// True and False are the stored spellings of the flag's two sides.
	True, False string
	// TrueLabel and FalseLabel are the painted words when they differ from
	// the stored spellings.
	TrueLabel, FalseLabel string
	Beta                  bool
	// Proactive writes the row through the store's typed coordination call.
	Proactive bool
}

var onOff = []string{"on", "off"}

// table is every key Settings keeps, in the order settings list prints them.
var table = []Setting{
	{Spec: Spec{Key: DefaultToolSetting, Row: "default tool"}, Field: FieldTool},
	{Spec: Spec{Key: ThemeSetting, Values: paletteNames(), Default: uitheme.Palettes[0].Name, Row: "theme"}, Field: FieldTheme},
	{Spec: Spec{Key: ThemeAutoSetting, Values: onOff, Default: "off", Row: "theme follows OS"}, Field: FieldThemeAuto,
		Flag: func(s *State) *bool { return &s.ThemeAuto }, True: "on", False: "off"},
	{Spec: Spec{Key: GlyphModeSetting, Values: []string{string(presentation.GlyphModeBasic), string(presentation.GlyphModeAdvanced)}, Default: string(presentation.DefaultGlyphMode), Row: "symbols"}, Field: FieldGlyphs},
	{Spec: Spec{Key: BackgroundSetting, Values: []string{"theme", "terminal"}, Default: "theme", Row: "background"}, Field: FieldBackground,
		Flag: func(s *State) *bool { return &s.TerminalBackground }, True: "terminal", False: "theme"},
	{Spec: Spec{Key: ListDensitySetting, Values: []string{"compact", "comfortable"}, Default: "compact", Row: "list density"}, Field: FieldDensity,
		Flag: func(s *State) *bool { return &s.ComfortableRows }, True: "comfortable", False: "compact"},
	{Spec: Spec{Key: SessionLayoutSetting, Values: []string{"split", "full"}, Default: "split", Row: "sessions layout"}, Field: FieldSessionLayout,
		Flag: func(s *State) *bool { return &s.FullLayout }, True: "full", False: "split", TrueLabel: "full screen"},
	{Spec: Spec{Key: HideHeaderSetting, Values: onOff, Default: "off", Row: "header"}, Field: FieldHeader,
		Flag: func(s *State) *bool { return &s.HideHeader }, True: "on", False: "off", TrueLabel: "hide", FalseLabel: "show"},
	{Spec: Spec{Key: HideStatsSetting, Values: onOff, Default: "off", Row: "computer stats"}, Field: FieldStats,
		Flag: func(s *State) *bool { return &s.HideStats }, True: "on", False: "off", TrueLabel: "hide", FalseLabel: "show"},
	{Spec: Spec{Key: DiffLayoutSetting, Values: []string{"split", "unified"}, Default: "split", Row: "review layout"}, Field: FieldLayout,
		Flag: func(s *State) *bool { return &s.LayoutSplit }, True: "split", False: "unified"},
	{Spec: Spec{Key: QuickCloseSetting, Values: []string{"stay", "close"}, Default: "stay", Row: "after quick prompt"}, Field: FieldQuickClose,
		Flag: func(s *State) *bool { return &s.QuickCloseSend }, True: "close", False: "stay", FalseLabel: "stay open"},
	{Spec: Spec{Key: FocusKeySetting, Values: []string{"focus", "attach"}, Default: "focus", Row: "session keys"}, Field: FieldFocusKey,
		Flag: func(s *State) *bool { return &s.EnterFocuses }, True: "focus", False: "attach", TrueLabel: "↵ focus · A attach", FalseLabel: "↵ attach · A focus"},
	{Spec: Spec{Key: ArrowStepSetting, Values: onOff, Default: "on", Row: "←→ step in/out"}, Field: FieldArrowStep,
		Flag: func(s *State) *bool { return &s.ArrowStep }, True: "on", False: "off", Beta: true},
	{Spec: Spec{Key: MouseSetting, Values: onOff, Default: "on", Row: "mouse"}, Field: FieldMouse,
		Flag: func(s *State) *bool { return &s.MouseDisabled }, True: "off", False: "on"},
	{Spec: Spec{Key: WorktreeSetting, Values: onOff, Default: "off", Row: "spawn in worktree"}, Field: FieldWorktree,
		Flag: func(s *State) *bool { return &s.WorktreeDefault }, True: "on", False: "off"},
	{Spec: Spec{Key: BaseFetchSetting, Values: onOff, Default: "on", Row: "fetch on spawn"}, Field: FieldBaseFetch,
		Flag: func(s *State) *bool { return &s.BaseFetch }, True: "on", False: "off"},
	{Spec: Spec{Key: CoordinationSetting, Values: []string{"on-request", "proactive"}, Default: "on-request", Row: "coordination"}, Field: FieldCoordination,
		Flag: func(s *State) *bool { return &s.Proactive }, True: "on", False: "off", TrueLabel: "proactive", FalseLabel: "on request", Proactive: true},
	{Spec: Spec{Key: NotificationsSetting, Values: onOff, Default: "on", Row: "notifications"}, Field: FieldNotify,
		Flag: func(s *State) *bool { return &s.Notifications }, True: "on", False: "off"},
	{Spec: Spec{Key: NotifyFinishedSetting, Values: onOff, Default: "off", Row: "notify on finish"}, Field: FieldNotifyFinish,
		Flag: func(s *State) *bool { return &s.NotifyFinished }, True: "on", False: "off"},
	{Spec: Spec{Key: NotifyCommandSetting}},
	{Spec: Spec{Key: EditorSetting, Row: "editor"}, Field: FieldEditor},
	{Spec: Spec{Key: HiddenToolsSetting, List: true, Row: "CLIs"}, Field: FieldCLIs},
}

// byField indexes the dialog rows; action rows and the shell-only key
// leave their slot nil.
var byField = func() [FieldCount]*Setting {
	var fields [FieldCount]*Setting
	for i := range table {
		if table[i].Row != "" {
			fields[table[i].Field] = &table[i]
		}
	}
	return fields
}()

func byKey(key string) *Setting {
	for i := range table {
		if table[i].Key == key {
			return &table[i]
		}
	}
	return nil
}

func paletteNames() []string {
	names := make([]string, len(uitheme.Palettes))
	for i, theme := range uitheme.Palettes {
		names[i] = theme.Name
	}
	return names
}

// saved reports a key the dialog writes with the other preferences: the
// shell-only key and the hidden CLIs, which have their own save, are not.
func (s Setting) saved() bool {
	return s.Row != "" && !s.List
}

// on reads a stored spelling as the flag's side. An unknown or empty
// value falls to the default side, which is the true side when the spec's
// default spells it.
func (s Setting) on(stored string) bool {
	if s.Default == s.True {
		return stored != s.False
	}
	return stored == s.True
}

func (s Setting) spelling(on bool) string {
	if on {
		return s.True
	}
	return s.False
}

func (s Setting) label(on bool) string {
	switch {
	case on && s.TrueLabel != "":
		return s.TrueLabel
	case !on && s.FalseLabel != "":
		return s.FalseLabel
	}
	return s.spelling(on)
}

// Keys lists every key Settings keeps, in the order settings list prints them.
func Keys() []string {
	keys := make([]string, len(table))
	for i, setting := range table {
		keys[i] = setting.Key
	}
	return keys
}

// FlagKeys lists the two-way rows' keys.
func FlagKeys() []string {
	keys := make([]string, 0, len(table))
	for _, setting := range table {
		if setting.Flag != nil {
			keys = append(keys, setting.Key)
		}
	}
	return keys
}

// FlagOn reads a two-way row's stored spelling as its side; any other key
// reads false.
func FlagOn(key, stored string) bool {
	setting := byKey(key)
	return setting != nil && setting.Flag != nil && setting.on(stored)
}

// Flag is the dialog's side of a two-way row; any other key reads false.
func (s *State) Flag(key string) bool {
	setting := byKey(key)
	return setting != nil && setting.Flag != nil && *setting.Flag(s)
}
