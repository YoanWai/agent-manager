package persist

import (
	"fmt"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// Target is the surface a store read refreshes: the dialog, the New Session
// form or the quick bar. Each keeps its own generation.
type Target uint8

const (
	// DialogTarget is the Settings card.
	DialogTarget Target = iota
	FormTarget
	QuickTarget
)

// LoadRequest is one store read: the surface that asked, its generation,
// and the keys outside the dialog read alongside it.
type LoadRequest struct {
	Target     Target
	Generation uint64
	Extra      []string
}

// LoadResult is what a read found. A key whose read failed keeps its cached
// value and carries the error.
type LoadResult struct {
	Values    []uisettings.Readback
	HiddenRaw string
	HiddenErr error
}

// LoadedMsg is a finished read, carrying its request so the surface that
// asked is the one that takes it.
type LoadedMsg struct {
	Request LoadRequest
	Result  LoadResult
	Err     error
}

// Load reads every key the dialog writes plus the hidden-CLI set.
func Load(reader Reader) (LoadResult, error) {
	values, hiddenRaw, hiddenErr := SettingsReadback(reader)
	result := LoadResult{Values: values, HiddenRaw: hiddenRaw, HiddenErr: hiddenErr}
	failed := 0
	var first error
	for _, value := range values {
		if value.Err != nil {
			failed++
			if first == nil {
				first = fmt.Errorf("%s: %w", value.Key, value.Err)
			}
		}
	}
	if hiddenErr != nil {
		failed++
		if first == nil {
			first = fmt.Errorf("%s: %w", uisettings.HiddenToolsSetting, hiddenErr)
		}
	}
	if first != nil {
		return result, fmt.Errorf("settings load: %d reads failed; first: %w", failed, first)
	}
	return result, nil
}

// LoadCmd reads the store off the update path.
func LoadCmd(request LoadRequest, reader Reader) tea.Cmd {
	return func() tea.Msg {
		result, err := Load(reader)
		result.Values = append(result.Values, ReadKeys(reader, request.Extra)...)
		return LoadedMsg{Request: request, Result: result, Err: err}
	}
}

// SettingsReadback reads every committed key back, with a per-key read
// error, so a completion never presents a failed read as the committed
// value. The stored coordination value is the marker
// SetProactiveCoordination writes; the dialog reads it as on or off.
func SettingsReadback(reader Reader) ([]uisettings.Readback, string, error) {
	values := ReadKeys(reader, uisettings.ReadbackKeys)
	coordValue, coordErr := reader.Setting(uisettings.CoordinationSetting)
	coord := "off"
	if coordErr == nil && coordValue == "proactive" {
		coord = "on"
	}
	values = append(values, uisettings.Readback{Key: uisettings.CoordinationSetting, Value: coord, Err: coordErr})
	hidden, hiddenErr := reader.Setting(uisettings.HiddenToolsSetting)
	return values, hidden, hiddenErr
}

// Take applies a read to the cache.
func (s *State) Take(result LoadResult) {
	s.Cache.ApplyReadback(result.Values, result.HiddenRaw, result.HiddenErr)
}

// Fresh reports whether generation may still rebuild its surface: no later
// visit or accepted save has run, and no save is on the lane.
func (s State) Fresh(generation uint64) bool { return s.Gen == generation && s.Pending == 0 }

// ReadFailureNote explains how many committed-value reads failed, so the
// prefs that kept their state are never silently stale. An empty note means
// every read worked.
func ReadFailureNote(failed int) string {
	if failed == 0 {
		return ""
	}
	return fmt.Sprintf("%d committed-value reads failed; the prefs keep their current state for them", failed)
}
