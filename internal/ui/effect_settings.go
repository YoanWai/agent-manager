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

// settingWriter is the store seam the settings worker writes through;
// tests script a failing write to prove the partial outcome.
type settingWriter interface {
	set(key, value string) error
	setProactive(proactive bool) error
	get(key string) (string, error)
}

type storeSettingWriter struct{ st *store.Store }

func (w storeSettingWriter) set(key, value string) error { return w.st.SetSetting(key, value) }
func (w storeSettingWriter) setProactive(proactive bool) error {
	return w.st.SetProactiveCoordination(proactive)
}
func (w storeSettingWriter) get(key string) (string, error) { return w.st.Setting(key) }

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
func settingsReadback(writer settingWriter) ([]restoredValue, string, error) {
	keys := []string{
		"default_tool", themeSetting, themeAutoSetting, diffLayoutSetting,
		quickCloseSetting, focusKeySetting, arrowStepSetting, listDensitySetting,
		sessionLayoutSetting, hideHeaderSetting, hideStatsSetting, mouseSetting,
		worktreeSetting, notificationsSetting, notifyFinishedSetting,
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

// applySettingsEffect reconciles the worker's durable outcome. Prefs are
// runtime state, not dialog state: a failure's committed read-back
// reconciles them on every completion, stale included — the lane's FIFO
// order lets a later save's completion win, so an earlier failure's
// applied-preference mismatch is never hidden behind a generation fence.
// Dialog fields are restored only while the request is the latest
// accepted save, so a newer dialog is never replaced.
func (m *Model) applySettingsEffect(job *effectJob, result settingsEffectResult, err error) tea.Cmd {
	request := job.request.(settingsRequest)
	stale := m.settingsGen != request.generation
	if err != nil {
		m.errBar.text = err.Error()
		if result.restored != nil {
			if note := m.reconcileSettingsPrefs(result.restored); note != "" {
				m.errBar.text += "; " + note
			}
			if !stale {
				m.restoreSettingsDialog(result.restored, result.restoredHidden, result.hiddenErr)
			}
		}
	}
	if err == nil {
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
func (m *Model) restoreSettingsDialog(restored []restoredValue, hiddenRaw string, hiddenErr error) {
	for _, value := range restored {
		if value.err != nil {
			continue
		}
		switch value.key {
		case "default_tool":
			for i, name := range m.settings.toolNames {
				if name == value.value {
					m.settings.toolIndex = i
					break
				}
			}
		case themeSetting:
			m.settings.themeIndex = themeIndex(value.value)
			if !m.settings.themeAuto {
				m.settings.manualTheme = value.value
			}
		case themeAutoSetting:
			m.settings.themeAuto = value.value == "on"
		case diffLayoutSetting:
			m.settings.layoutSplit = value.value == "split"
		case quickCloseSetting:
			m.settings.quickCloseSend = value.value == "close"
		case focusKeySetting:
			m.settings.enterFocuses = value.value == "focus"
		case arrowStepSetting:
			m.settings.arrowStep = value.value == "on"
		case listDensitySetting:
			m.settings.comfortableRows = value.value == "comfortable"
		case sessionLayoutSetting:
			m.settings.fullLayout = value.value == sessionLayoutValue(true)
		case hideHeaderSetting:
			m.settings.hideHeader = value.value == "on"
		case hideStatsSetting:
			m.settings.hideStats = value.value == "on"
		case mouseSetting:
			m.settings.mouseDisabled = value.value == "off"
		case worktreeSetting:
			m.settings.worktreeDefault = value.value == "on"
		case notificationsSetting:
			m.settings.notifications = value.value == "on"
		case notifyFinishedSetting:
			m.settings.notifyFinished = value.value == "on"
		case "coordination":
			m.settings.proactive = value.value == "on"
		}
	}
	// An empty committed hidden state is a real state: it clears the
	// dialog's map. Only a failed read leaves the old map in place.
	if hiddenErr == nil {
		m.settings.cliHidden = parseHiddenTools(hiddenRaw)
		if m.settings.cliHidden == nil {
			m.settings.cliHidden = map[string]bool{}
		}
	}
}
