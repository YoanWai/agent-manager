// Package persist is the Settings runtime: the cache of the stored values,
// the read that refreshes it, the writes a save hands the effect lane, and
// the read-back a partial failure reconciles from. The dialog in package
// settings paints what State caches; the root routes its messages here and
// answers Root.
package persist

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

type Reader interface {
	Setting(key string) (string, error)
}

type Writer interface {
	Reader
	SetSetting(key, value string) error
	SetProactiveCoordination(proactive bool) error
}

type Presets interface {
	SessionPresets() ([]store.SessionPreset, error)
	SaveSessionPreset(previous string, preset store.SessionPreset) error
	DeleteSessionPreset(name string) (bool, error)
}

type StoreWriter struct{ Store *store.Store }

func (w StoreWriter) Setting(key string) (string, error) { return w.Store.Setting(key) }

func (w StoreWriter) SetSetting(key, value string) error { return w.Store.SetSetting(key, value) }

func (w StoreWriter) SetProactiveCoordination(proactive bool) error {
	return w.Store.SetProactiveCoordination(proactive)
}

type Root interface {
	ReportErr(text string)
	// Caveat adds a second line of explanation to the failure already
	// reported.
	Caveat(text string)
	// MirrorPrefs reconciles the running manager to committed values and
	// reports how many reads failed, so their prefs keep their state.
	MirrorPrefs(values []uisettings.Readback) string
	Restore(values []uisettings.Readback, hiddenRaw string, hiddenErr error, editors *uisettings.EditorsProbe)
	Update(staged bool) tea.Cmd
}

// State is what the root keeps for Settings: the cache of the stored values,
// the saves still on the effect lane, and the generation that fences a load
// or a completion against the visit that asked.
type State struct {
	Cache uisettings.Cache
	// Pending counts the saves on the lane, so a store read never rebuilds
	// a dialog whose save is still out.
	Pending int
	// Gen is the latest Settings visit or accepted save.
	Gen uint64
}

func New(st Writer, extra []string) State {
	var state State
	result, _ := Load(st)
	state.Cache.ApplyReadback(append(result.Values, ReadKeys(st, extra)...), result.HiddenRaw, result.HiddenErr)
	return state
}

// ReadKeys reads keys into the cache's spelling. A symbols value the frame
// cannot paint is a read error, so the dialog keeps what it shows.
func ReadKeys(reader Reader, keys []string) []uisettings.Readback {
	values := make([]uisettings.Readback, 0, len(keys))
	for _, key := range keys {
		value, err := reader.Setting(key)
		if err == nil && key == uisettings.GlyphModeSetting && value != "" &&
			value != string(presentation.GlyphModeBasic) && value != string(presentation.GlyphModeAdvanced) {
			err = fmt.Errorf("unknown symbols setting: %s", value)
		}
		values = append(values, uisettings.Readback{Key: key, Value: value, Err: err})
	}
	return values
}
