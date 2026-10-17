package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsRequest captures the dialog's chosen preferences and CLI
// visibility at dispatch; the worker persists them outside Update in the
// order the dialog used to write them.
type settingsRequest struct {
	values       []settingValue
	hidden       []string
	followUpdate bool
	generation   uint64
}

func (settingsRequest) effectRequest() {}

// settingsEffectResult reports what the worker durably committed. On a
// partial failure, restored carries the read-back of the committed keys
// with a per-key read error: a failed read must not be presented as the
// committed value. nil restored means every write committed.
type settingsEffectResult struct {
	generation     uint64
	committed, tot int
	restored       []restoredValue
	restoredHidden string
	hiddenErr      error
}

func (settingsEffectResult) effectResult() {}

type settingsLoadTarget uint8

const (
	settingsLoadDialog settingsLoadTarget = iota
	settingsLoadForm
	settingsLoadQuick
)

type settingsLoadRequest struct {
	target     settingsLoadTarget
	generation uint64
	// extra are keys outside the dialog, read into the cache alongside it.
	extra []string
}

type settingsLoadResult struct {
	values    []restoredValue
	hiddenRaw string
	hiddenErr error
}

type settingsLoadedMsg struct {
	request settingsLoadRequest
	result  settingsLoadResult
	err     error
}

type settingsValueReader interface {
	get(key string) (string, error)
}

// settingWriter is the store seam the settings worker writes through;
// tests script a failing write to prove the partial outcome.
type settingWriter interface {
	settingsValueReader
	set(key, value string) error
	setProactive(proactive bool) error
}

type storeSettingWriter struct{ st *store.Store }

func (w storeSettingWriter) set(key, value string) error { return w.st.SetSetting(key, value) }
func (w storeSettingWriter) setProactive(proactive bool) error {
	return w.st.SetProactiveCoordination(proactive)
}
func (w storeSettingWriter) get(key string) (string, error) { return w.st.Setting(key) }

func loadSettingsCache(st *store.Store, extra []string) settingsCache {
	reader := storeSettingWriter{st: st}
	result, _ := loadSettingsWithReader(reader)
	cache := settingsCache{values: make(map[string]string)}
	cache.applyReadback(append(result.values, readSettingKeys(reader, extra)...), result.hiddenRaw, result.hiddenErr)
	return cache
}

func readSettingKeys(reader settingsValueReader, keys []string) []restoredValue {
	values := make([]restoredValue, 0, len(keys))
	for _, key := range keys {
		value, err := reader.get(key)
		if err == nil && key == uisettings.GlyphModeSetting && value != "" && value != string(presentation.GlyphModeBasic) && value != string(presentation.GlyphModeAdvanced) {
			err = fmt.Errorf("unknown symbols setting: %s", value)
		}
		values = append(values, restoredValue{Key: key, Value: value, Err: err})
	}
	return values
}

func loadSettingsWithReader(reader settingsValueReader) (settingsLoadResult, error) {
	values, hiddenRaw, hiddenErr := settingsReadback(reader)
	result := settingsLoadResult{values: values, hiddenRaw: hiddenRaw, hiddenErr: hiddenErr}
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

func settingsLoadCmd(request settingsLoadRequest, reader settingsValueReader) tea.Cmd {
	return func() tea.Msg {
		result, err := loadSettingsWithReader(reader)
		result.values = append(result.values, readSettingKeys(reader, request.extra)...)
		return settingsLoadedMsg{request: request, result: result, err: err}
	}
}

func (s effectServices) runSettings(request settingsRequest) (effectResult, error) {
	return runSettingsWithWriter(request, storeSettingWriter{st: s.store})
}

func runSettingsWithWriter(request settingsRequest, writer settingWriter) (effectResult, error) {
	result := settingsEffectResult{generation: request.generation}
	result.tot = len(request.values)
	if request.hidden != nil {
		result.tot++
	}
	for _, value := range request.values {
		var err error
		if value.Proactive {
			err = writer.setProactive(value.Value == "on")
		} else {
			err = writer.set(value.Key, value.Value)
		}
		if err != nil {
			result.restored, result.restoredHidden, result.hiddenErr = settingsReadback(writer)
			return result, fmt.Errorf("settings save committed %d of %d writes: %w", result.committed, result.tot, err)
		}
		result.committed++
	}
	if request.hidden != nil {
		raw := strings.Join(request.hidden, ",")
		if err := writer.set(uisettings.HiddenToolsSetting, raw); err != nil {
			result.restored, result.restoredHidden, result.hiddenErr = settingsReadback(writer)
			return result, fmt.Errorf("settings save committed %d of %d writes: %w", result.committed, result.tot, err)
		}
		result.committed++
	}
	return result, nil
}

// settingsReadback returns the committed keys' values after a partial
// failure, with a per-key read error, so completion never presents a
// failed read as the committed value.
func settingsReadback(writer settingsValueReader) ([]restoredValue, string, error) {
	values := readSettingKeys(writer, uisettings.ReadbackKeys)
	// The stored coordination value is the proactive marker
	// SetProactiveCoordination writes; the dialog reads it as on or off.
	coordValue, coordErr := writer.get(uisettings.CoordinationSetting)
	coord := "off"
	if coordErr == nil && coordValue == "proactive" {
		coord = "on"
	}
	values = append(values, restoredValue{Key: uisettings.CoordinationSetting, Value: coord, Err: coordErr})
	hidden, hiddenErr := writer.get(uisettings.HiddenToolsSetting)
	return values, hidden, hiddenErr
}

// settingsCache mirrors the store's settings so frames and the dialog read
// them without a store call.
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

func (c *settingsCache) ensureValues() {
	if c.values == nil {
		c.values = make(map[string]string)
	}
}

func (c *settingsCache) applyValues(values []settingValue) {
	c.ensureValues()
	for _, value := range values {
		c.values[value.Key] = value.Value
	}
}

func (c *settingsCache) applyHidden(names []string) {
	c.hidden = make(map[string]bool, len(names))
	for _, name := range names {
		c.hidden[name] = true
	}
}

func (c *settingsCache) applyReadback(values []restoredValue, hiddenRaw string, hiddenErr error) {
	c.ensureValues()
	for _, value := range values {
		if value.Err == nil {
			c.values[value.Key] = value.Value
		}
	}
	if hiddenErr == nil {
		c.hidden = uisettings.ParseHidden(hiddenRaw)
	}
}

func (m *Model) handleSettingsLoaded(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	var follow tea.Cmd
	switch msg.request.target {
	case settingsLoadDialog:
		if msg.request.generation != m.settings.gen || m.mode != modeSettings ||
			m.settings.dialog.Busy() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		field := m.settings.dialog.Field
		m.settings.dialog = m.settings.stateFromCache(m.services.cfg)
		// A manual theme the shell changed meanwhile repaints here, so esc
		// keeps it instead of writing back the palette cached at startup.
		if d := &m.settings.dialog; !d.ThemeAuto && current().Name != d.ManualTheme {
			d.ShowTheme(d.ManualTheme)
			applyTheme(themes[d.ThemeIndex])
			SyncTerminalColors()
			follow = m.syncPaneTheme()
		}
		m.settings.dialog.MarkBaseline()
		m.settings.dialog.Field = field
	case settingsLoadForm:
		if m.mode != modeForm || uint64(m.form.prompt.gen) != msg.request.generation ||
			m.form.DefaultsTouched() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshFormChoicePrefs()
		follow = tea.Batch(m.applyCachedFormDefaults(), m.formWorktreeProbeCmd(false))
	case settingsLoadQuick:
		if !m.quick.active || uint64(m.quick.gen) != msg.request.generation ||
			m.quick.DefaultsTouched() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshQuickChoicePrefs()
		follow = tea.Batch(m.applyCachedQuickDefaults(), m.quickWorktreeProbeCmd(false))
	default:
		return m, nil
	}
	m.applyCachedSettingsPrefs()
	m.rebuildRows()
	if msg.err != nil {
		m.reportErr(msg.err.Error())
	} else {
		m.clearErr()
	}
	return m, follow
}

// applySettingsEffect reconciles the worker's durable outcome. Prefs are
// runtime state, not dialog state: a failure's committed read-back
// reconciles them on every completion, stale included — the lane's FIFO
// order lets a later save's completion win, so an earlier failure's
// applied-preference mismatch is never hidden behind a generation fence.
// Dialog fields are restored only while the request is the latest
// accepted save, so a newer dialog is never replaced.
func (m *Model) applySettingsEffect(job *effectJob, result settingsEffectResult, err error) tea.Cmd {
	request := job.request.(settingsRequest)
	if m.settings.pending > 0 {
		m.settings.pending--
	}
	stale := m.settings.gen != request.generation
	if err != nil {
		m.reportErr(err.Error())
		if result.restored != nil {
			m.settings.cache.applyReadback(result.restored, result.restoredHidden, result.hiddenErr)
			if note := m.reconcileSettingsPrefs(result.restored); note != "" {
				m.reportErr(m.errBar.text + "; " + note)
			}
			if !stale {
				m.settings.dialog.Restore(result.restored, result.restoredHidden, result.hiddenErr, m.settings.cache.editors)
			}
		}
	}
	if err == nil {
		m.settings.cache.applyValues(request.values)
		if request.hidden != nil {
			m.settings.cache.applyHidden(request.hidden)
		}
		committed := make([]restoredValue, 0, len(request.values))
		for _, value := range request.values {
			committed = append(committed, restoredValue{Key: value.Key, Value: value.Value})
		}
		m.reconcileSettingsPrefs(committed)
	}
	if request.followUpdate {
		if err != nil {
			m.update.applying = false
			return nil
		}
		return m.applyUpdateCmd()
	}
	return nil
}

// reconcileSettingsPrefs restores the live-session prefs to the committed
// read-back; keys whose read failed keep their current value.
func (m *Model) reconcileSettingsPrefs(restored []restoredValue) string {
	failed := 0
	for _, value := range restored {
		if value.Err != nil {
			failed++
			continue
		}
		m.mirrorPreference(value.Key, value.Value)
	}
	if failed == 0 {
		return ""
	}
	return fmt.Sprintf("%d committed-value reads failed; the prefs keep their current state for them", failed)
}
