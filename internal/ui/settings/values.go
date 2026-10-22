package settings

import (
	"slices"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
)

// Value is one store write in persist order. Proactive writes the
// coordination setting through the typed store call.
type Value struct {
	Key       string
	Value     string
	Proactive bool
}

// Readback is one committed key's read-back; Err means the read failed and
// the dialog keeps its current state for that key.
type Readback struct {
	Key   string
	Value string
	Err   error
}

// ReadbackKeys are the keys a partial failure reads back, in order; the
// coordination key follows them through its own spelling.
var ReadbackKeys = func() []string {
	keys := make([]string, 0, len(table))
	for _, setting := range table {
		if setting.saved() && !setting.Proactive {
			keys = append(keys, setting.Key)
		}
	}
	return keys
}()

// saveOrder is the order a save has always written the rows in, so a save
// that fails midway commits the same prefix as before; a row it does not
// name follows in table order.
var saveOrder = func() []Setting {
	named := []string{
		DefaultToolSetting, ThemeSetting, ThemeAutoSetting, DiffLayoutSetting, QuickCloseSetting,
		FocusKeySetting, ArrowStepSetting, ListDensitySetting, SessionLayoutSetting, HideHeaderSetting,
		HideStatsSetting, BackgroundSetting, MouseSetting, WorktreeSetting, BaseFetchSetting,
		CoordinationSetting, NotificationsSetting, NotifyFinishedSetting, EditorSetting, GlyphModeSetting,
	}
	ordered := make([]Setting, 0, len(table))
	for _, key := range named {
		ordered = append(ordered, *byKey(key))
	}
	for _, setting := range table {
		if setting.saved() && !slices.Contains(named, setting.Key) {
			ordered = append(ordered, setting)
		}
	}
	return ordered
}()

// ParseHidden reads the stored hidden-CLI list.
func ParseHidden(raw string) map[string]bool {
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

// MarkBaseline records the freshly built dialog as the state a save
// compares against.
func (s *State) MarkBaseline() {
	s.Baseline = make(map[string]string)
	for _, value := range s.Values() {
		s.Baseline[value.Key] = value.Value
	}
	hidden := strings.Join(s.HiddenList(), ",")
	s.BaselineHidden = &hidden
}

// ChangedValues is the write list narrowed to the keys the user changed in
// this dialog.
func (s *State) ChangedValues() []Value {
	all := s.Values()
	changed := all[:0:0]
	for _, value := range all {
		if before, ok := s.Baseline[value.Key]; !ok || before != value.Value {
			changed = append(changed, value)
		}
	}
	return changed
}

func (s *State) HiddenAtBaseline(hidden []string) bool {
	return s.BaselineHidden != nil && *s.BaselineHidden == strings.Join(hidden, ",")
}

// AdvanceBaseline moves the baseline to a save's captured values, so a
// dialog left open after it (the update row) writes only later changes.
func (s *State) AdvanceBaseline(values []Value, hidden []string) {
	if s.Baseline == nil {
		s.Baseline = make(map[string]string)
	}
	for _, value := range values {
		s.Baseline[value.Key] = value.Value
	}
	if hidden != nil {
		raw := strings.Join(hidden, ",")
		s.BaselineHidden = &raw
	}
}

// HiddenList is the picker's hidden set in persist order (sorted names,
// comma-joined by the worker).
func (s *State) HiddenList() []string {
	names := make([]string, 0, len(s.CLIHidden))
	for name, on := range s.CLIHidden {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Values copies the dialog's chosen preferences into the ordered write
// list the effect worker persists.
func (s *State) Values() []Value {
	values := make([]Value, 0, len(saveOrder))
	for _, setting := range saveOrder {
		switch setting.Key {
		case DefaultToolSetting:
			if len(s.ToolNames) > 0 {
				values = append(values, Value{Key: DefaultToolSetting, Value: s.ToolNames[s.ToolIndex]})
			}
		case ThemeSetting:
			values = append(values, Value{Key: ThemeSetting, Value: s.storedTheme()})
		case GlyphModeSetting:
			values = append(values, Value{Key: GlyphModeSetting, Value: string(s.GlyphMode.Effective())})
		case EditorSetting:
			values = append(values, Value{Key: EditorSetting, Value: s.Editor.Line()})
		default:
			values = append(values, Value{Key: setting.Key, Value: setting.spelling(*setting.Flag(s)), Proactive: setting.Proactive})
		}
	}
	return values
}

// storedTheme is the theme key's value. With auto-detect on, the picker
// shows the detected theme; the key keeps the manual choice so turning
// auto off returns to it.
func (s *State) storedTheme() string {
	if s.ThemeAuto {
		return s.ManualTheme
	}
	return uitheme.Palettes[s.ThemeIndex].Name
}

// Restore applies committed store values back to a live dialog after a
// partial failure, so the dialog shows what persisted. Keys whose read
// failed keep their current state. editors is the last PATH probe, for the
// editor row the restored line rebuilds.
func (s *State) Restore(restored []Readback, hiddenRaw string, hiddenErr error, editors *EditorsProbe) {
	for _, value := range restored {
		if value.Err != nil {
			continue
		}
		switch value.Key {
		case GlyphModeSetting:
			s.GlyphMode = presentation.GlyphMode(value.Value)
		case DefaultToolSetting:
			for i, name := range s.ToolNames {
				if name == value.Value {
					s.ToolIndex = i
					break
				}
			}
		case ThemeSetting:
			s.ThemeIndex = uitheme.Index(value.Value)
			if !s.ThemeAuto {
				s.ManualTheme = value.Value
			}
		case EditorSetting:
			s.Editor = NewEditorRow(value.Value, editors)
		default:
			if setting := byKey(value.Key); setting != nil && setting.Flag != nil {
				*setting.Flag(s) = setting.on(value.Value)
			}
		}
	}
	// An empty committed hidden state is a real state: it clears the
	// dialog's map. Only a failed read leaves the old map in place.
	if hiddenErr == nil {
		s.CLIHidden = ParseHidden(hiddenRaw)
		if s.CLIHidden == nil {
			s.CLIHidden = map[string]bool{}
		}
	}
	// The restored keys now show the store, so they match the baseline;
	// a key whose read failed leaves it, so the next save writes it.
	if s.Baseline == nil {
		s.Baseline = make(map[string]string)
	}
	shown := make(map[string]string)
	for _, value := range s.Values() {
		shown[value.Key] = value.Value
	}
	for _, value := range restored {
		if value.Err != nil {
			delete(s.Baseline, value.Key)
		} else if v, ok := shown[value.Key]; ok {
			s.Baseline[value.Key] = v
		}
	}
	s.BaselineHidden = nil
	if hiddenErr == nil {
		raw := strings.Join(s.HiddenList(), ",")
		s.BaselineHidden = &raw
	}
}
