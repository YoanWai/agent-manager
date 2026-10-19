package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

func TestSymbolsExplicitChoiceAndPendingReopen(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	_, save := m.saveAndCloseSettings()
	m.applyCmd(t, save)
	if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "" {
		t.Fatal("untouched settings pinned symbols default")
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		m.applyCmd(t, m.openSettings())
		m.settings.dialog.Field = uisettings.FieldGlyphs
		m.handleSettingsKey(key("right"))
		if m.prefs.glyphMode != mode {
			t.Fatal("symbols did not preview")
		}
		_, save = m.saveAndCloseSettings()
		m.openSettings()
		if m.settings.dialog.GlyphMode != mode {
			t.Fatal("reopen lost pending symbols")
		}
		_, unchanged := m.saveAndCloseSettings()
		m.applyCmd(t, save)
		m.applyCmd(t, unchanged)
		m.drainEffects(t)
		if got := reloadModel(t, m).prefs.glyphMode; got != mode {
			t.Fatalf("saved symbols %q want %q", got, mode)
		}
	}
	fresh := buildModel(t)
	fresh.applyCmd(t, fresh.openSettings())
	fresh.settings.dialog.Field = uisettings.FieldGlyphs
	fresh.handleSettingsKey(key("right"))
	fresh.handleSettingsKey(key("left"))
	_, save = fresh.saveAndCloseSettings()
	fresh.applyCmd(t, save)
	if raw, _ := fresh.services.store.Setting(uisettings.GlyphModeSetting); raw != "advanced" {
		t.Fatal("cycling back did not pin explicit default")
	}
}

func TestSymbolsMouseSaveUsesPaintedRows(t *testing.T) {
	for _, width := range []int{34, 80, 160} {
		m := buildModel(t)
		m.layout.width, m.layout.height = width, 24
		m.applyCmd(t, m.openSettings())
		preparedView(m)
		m = leftClick(m, m.layout.card.bodyLeft-1, m.layout.card.bodyTop)
		if m.prefs.glyphMode != "" {
			t.Fatal("padding changed symbols")
		}
		if m, _ = clickBody(t, m, "symbols"); m.prefs.glyphMode != presentation.GlyphModeBasic {
			t.Fatal("symbols click missed")
		}
		if !paints(m, "Compatible") {
			t.Fatal("symbols label did not update")
		}
		m, save := clickBody(t, m, "Save and close")
		m.applyCmd(t, save)
		if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "basic" || m.mode != modeList {
			t.Fatal("mouse save did not persist and close")
		}
	}
}

func TestSymbolsRejectInvalidAndStaleReads(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	stale := settingsLoadedMsg{request: settingsLoadRequest{target: settingsLoadDialog, generation: m.settings.gen}, result: settingsLoadResult{values: []restoredValue{{Key: uisettings.GlyphModeSetting, Value: "advanced"}}}}
	m.settings.dialog.Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	m.handleSettingsLoaded(stale)
	if m.prefs.glyphMode != presentation.GlyphModeBasic {
		t.Fatal("load overwrote unsaved symbols")
	}
	_, save := m.saveAndCloseSettings()
	m.applyCmd(t, save)
	if err := m.services.store.SetSetting(uisettings.GlyphModeSetting, "unknown"); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.openSettings())
	if m.prefs.glyphMode != presentation.GlyphModeBasic || !strings.Contains(m.errBar.text, "unknown symbols setting") {
		t.Fatal("invalid symbols were not rejected")
	}
}

func TestSymbolsSaveFailureRestoresAndCanRetry(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	m.settings.dialog.Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	request := settingsRequest{values: m.settings.dialog.ChangedValues(), generation: m.settings.gen}
	writer := &settingsScriptWriter{failOn: uisettings.GlyphModeSetting, values: map[string]string{}}
	result, err := runSettingsWithWriter(request, writer)
	if err == nil {
		t.Fatal("fixture write must fail")
	}
	m.applySettingsEffect(&effectJob{request: request}, result.(settingsEffectResult), err)
	if m.prefs.glyphMode != "" || m.errBar.text == "" {
		t.Fatal("failed symbols save did not restore default and report failure")
	}
	m.settings.dialog.Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	_, save := m.saveAndCloseSettings()
	m.applyCmd(t, save)
	if raw, _ := m.services.store.Setting(uisettings.GlyphModeSetting); raw != "basic" {
		t.Fatal("retry did not save")
	}
}

func TestSettingsUpdateDoesNotStartAfterSymbolsSaveFails(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openSettings())
	m.update.latest = "v0.3.0"
	m.settings.dialog.Field = uisettings.FieldGlyphs
	m.handleSettingsKey(key("right"))
	m.settings.dialog.Field = uisettings.FieldUpdate
	m.handleSettingsKey(key("enter"))
	job := m.effects.main.active
	request := job.request.(settingsRequest)
	if !request.followUpdate {
		t.Fatal("update not captured")
	}
	if cmd := m.applySettingsEffect(job, settingsEffectResult{}, errors.New("disk full")); cmd != nil || m.update.applying {
		t.Fatal("failed settings save still starts update")
	}
}
