package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
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

// settingValue is one store write in persist order. proactive writes the
// coordination setting through the typed store call.
type settingValue struct {
	key       string
	value     string
	proactive bool
}

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

// restoredValue is one committed key's read-back; err means the read
// failed and the dialog keeps its current state for that key.
type restoredValue struct {
	key   string
	value string
	err   error
}

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
		values = append(values, restoredValue{key: key, value: value, err: err})
	}
	return values
}

func loadSettingsWithReader(reader settingsValueReader) (settingsLoadResult, error) {
	values, hiddenRaw, hiddenErr := settingsReadback(reader)
	result := settingsLoadResult{values: values, hiddenRaw: hiddenRaw, hiddenErr: hiddenErr}
	failed := 0
	var first error
	for _, value := range values {
		if value.err != nil {
			failed++
			if first == nil {
				first = fmt.Errorf("%s: %w", value.key, value.err)
			}
		}
	}
	if hiddenErr != nil {
		failed++
		if first == nil {
			first = fmt.Errorf("%s: %w", hiddenToolsSetting, hiddenErr)
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
		if value.proactive {
			err = writer.setProactive(value.value == "on")
		} else {
			err = writer.set(value.key, value.value)
		}
		if err != nil {
			result.restored, result.restoredHidden, result.hiddenErr = settingsReadback(writer)
			return result, fmt.Errorf("settings save committed %d of %d writes: %w", result.committed, result.tot, err)
		}
		result.committed++
	}
	if request.hidden != nil {
		raw := strings.Join(request.hidden, ",")
		if err := writer.set(hiddenToolsSetting, raw); err != nil {
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
	keys := []string{
		"default_tool", themeSetting, themeAutoSetting, diffLayoutSetting,
		quickCloseSetting, focusKeySetting, arrowStepSetting, listDensitySetting,
		sessionLayoutSetting, hideHeaderSetting, hideStatsSetting, mouseSetting,
		worktreeSetting, notificationsSetting, notifyFinishedSetting,
		backgroundSetting, baseFetchSetting, editorSetting,
	}
	values := make([]restoredValue, 0, len(keys)+1)
	for _, key := range keys {
		value, err := writer.get(key)
		values = append(values, restoredValue{key: key, value: value, err: err})
	}
	// coordinationSetting lives in the store package; the value is the
	// proactive marker SetProactiveCoordination writes.
	coordValue, coordErr := writer.get("coordination")
	coord := "off"
	if coordErr == nil && coordValue == "proactive" {
		coord = "on"
	}
	values = append(values, restoredValue{key: "coordination", value: coord, err: coordErr})
	hidden, hiddenErr := writer.get(hiddenToolsSetting)
	return values, hidden, hiddenErr
}

func (c *settingsCache) ensureValues() {
	if c.values == nil {
		c.values = make(map[string]string)
	}
}

func (c *settingsCache) applyValues(values []settingValue) {
	c.ensureValues()
	for _, value := range values {
		c.values[value.key] = value.value
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
		if value.err == nil {
			c.values[value.key] = value.value
		}
	}
	if hiddenErr == nil {
		c.hidden = parseHiddenTools(hiddenRaw)
	}
}

func (m *Model) handleSettingsLoaded(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	var follow tea.Cmd
	switch msg.request.target {
	case settingsLoadDialog:
		if msg.request.generation != m.settings.gen || m.mode != modeSettings ||
			m.settings.dialog.dirty || m.settings.dialog.cliPicker || m.settings.dialog.keyPicker || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		field := m.settings.dialog.field
		m.settings.dialog = m.settings.settingsStateFromCache(m.services.cfg)
		m.settings.dialog.field = field
	case settingsLoadForm:
		if m.mode != modeForm || uint64(m.form.prompt.gen) != msg.request.generation ||
			m.form.defaultsTouched || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshChoicePrefs(m.formTool(), &m.form.choice)
		follow = tea.Batch(m.applyCachedFormDefaults(), m.formWorktreeProbeCmd(false))
	case settingsLoadQuick:
		if !m.quick.active || uint64(m.quick.gen) != msg.request.generation ||
			m.quick.defaultsTouched || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.applyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshChoicePrefs(m.quickTool(), &m.quick.choice)
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
				m.settings.restoreSettingsDialog(result.restored, result.restoredHidden, result.hiddenErr)
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
			committed = append(committed, restoredValue{key: value.key, value: value.value})
		}
		m.reconcileSettingsPrefs(committed)
	}
	if request.followUpdate {
		return m.applyUpdateCmd()
	}
	return nil
}

// reconcileSettingsPrefs restores the live-session prefs to the committed
// read-back; keys whose read failed keep their current value.
func (m *Model) reconcileSettingsPrefs(restored []restoredValue) string {
	failed := 0
	for _, value := range restored {
		if value.err != nil {
			failed++
			continue
		}
		switch value.key {
		case focusKeySetting:
			m.prefs.focusOnEnter = value.value == "focus"
		case arrowStepSetting:
			m.prefs.arrowStep = value.value == "on"
		case listDensitySetting:
			m.prefs.comfortableRows = value.value == "comfortable"
		case sessionLayoutSetting:
			m.prefs.fullLayout = value.value == sessionLayoutValue(true)
		case hideHeaderSetting:
			m.prefs.hideHeader = value.value == "on"
		case hideStatsSetting:
			m.prefs.hideStats = value.value == "on"
		case mouseSetting:
			m.prefs.mouseDisabled = value.value == "off"
		case backgroundSetting:
			m.prefs.terminalBackground = value.value == "terminal"
		case baseFetchSetting:
			m.prefs.baseFetchOff = value.value == "off"
		case editorSetting:
			m.services.editor = value.value
		}
	}
	if failed == 0 {
		return ""
	}
	return fmt.Sprintf("%d committed-value reads failed; the prefs keep their current state for them", failed)
}

// restoreSettingsDialog applies committed store values back to a live
// dialog after a partial failure, so the dialog shows what persisted.
// Keys whose read failed keep their current state.
func (s *settingsFeature) restoreSettingsDialog(restored []restoredValue, hiddenRaw string, hiddenErr error) {
	for _, value := range restored {
		if value.err != nil {
			continue
		}
		switch value.key {
		case "default_tool":
			for i, name := range s.dialog.toolNames {
				if name == value.value {
					s.dialog.toolIndex = i
					break
				}
			}
		case themeSetting:
			s.dialog.themeIndex = themeIndex(value.value)
			if !s.dialog.themeAuto {
				s.dialog.manualTheme = value.value
			}
		case themeAutoSetting:
			s.dialog.themeAuto = value.value == "on"
		case diffLayoutSetting:
			s.dialog.layoutSplit = value.value == "split"
		case quickCloseSetting:
			s.dialog.quickCloseSend = value.value == "close"
		case focusKeySetting:
			s.dialog.enterFocuses = value.value == "focus"
		case arrowStepSetting:
			s.dialog.arrowStep = value.value == "on"
		case listDensitySetting:
			s.dialog.comfortableRows = value.value == "comfortable"
		case sessionLayoutSetting:
			s.dialog.fullLayout = value.value == sessionLayoutValue(true)
		case hideHeaderSetting:
			s.dialog.hideHeader = value.value == "on"
		case hideStatsSetting:
			s.dialog.hideStats = value.value == "on"
		case mouseSetting:
			s.dialog.mouseDisabled = value.value == "off"
		case worktreeSetting:
			s.dialog.worktreeDefault = value.value == "on"
		case notificationsSetting:
			s.dialog.notifications = value.value == "on"
		case notifyFinishedSetting:
			s.dialog.notifyFinished = value.value == "on"
		case "coordination":
			s.dialog.proactive = value.value == "on"
		case backgroundSetting:
			s.dialog.terminalBackground = value.value == "terminal"
		case baseFetchSetting:
			s.dialog.baseFetch = value.value != "off"
		case editorSetting:
			s.dialog.editor = s.cachedEditorRow()
		}
	}
	// An empty committed hidden state is a real state: it clears the
	// dialog's map. Only a failed read leaves the old map in place.
	if hiddenErr == nil {
		s.dialog.cliHidden = parseHiddenTools(hiddenRaw)
		if s.dialog.cliHidden == nil {
			s.dialog.cliHidden = map[string]bool{}
		}
	}
}
