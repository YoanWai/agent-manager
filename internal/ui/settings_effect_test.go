package ui

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsScriptWriter scripts failing writes and reads so the partial
// outcome can be proven deterministically.
type settingsScriptWriter struct {
	failOn    string
	failGetOn string
	values    map[string]string
}

func (w *settingsScriptWriter) SetSetting(key, value string) error {
	if w.failOn == key {
		return errors.New("scripted write failure")
	}
	w.values[key] = value
	return nil
}

func (w *settingsScriptWriter) SetProactiveCoordination(proactive bool) error {
	if w.failOn == "coordination" {
		return errors.New("scripted write failure")
	}
	w.values["coordination"] = "proactive"
	return nil
}

func (w *settingsScriptWriter) Setting(key string) (string, error) {
	if w.failGetOn == key {
		return "", errors.New("scripted read failure")
	}
	return w.values[key], nil
}

// Stage 1 red: saving the dialog must not write the store on the update
// path; the accepted effect writes the captured choices on completion.
func TestSettingsEnterDefersStoreWrites(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name

	_, cmd := m.handleSettingsKey(key("enter"))
	if m.settingsOpen() {
		t.Fatalf("enter should close the dialog, screen = %v", m.screenID())
	}
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("theme written on the update path: %q", got)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want captured %q", got, staged)
	}
}

// Stage 1 red: leaving the CLI picker persists the captured visibility,
// not a store write inside the key handler.
func TestCLIPickerEscDefersHiddenWrite(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 1
	name := m.settingsCard().State().CLINames[1]
	m.handleSettingsKey(key(" "))
	if !m.settingsCard().State().CLIHidden[name] {
		t.Fatal("space should hide the focused CLI")
	}
	_, cmd := m.handleSettingsKey(key("esc"))
	if m.settingsCard().State().CLIPicker {
		t.Fatal("esc should leave the picker")
	}
	raw, err := m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatalf("hidden_tools written on the update path: %q", raw)
	}
	m.applyCmd(t, cmd)
	raw, err = m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != name {
		t.Fatalf("stored hidden_tools=%q, want captured %q", raw, name)
	}
}

// Stage 1 red: a save accepted while one choice is staged must keep that
// captured value; later dialog state may not retarget it.
func TestSettingsSaveKeepsCapturedChoices(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	captured := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, cmd := m.handleSettingsKey(key("enter"))

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	m.cycleSetting(1)
	m.applyCmd(t, cmd)

	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != captured {
		t.Fatalf("stored theme=%q, want captured %q", got, captured)
	}
}

// A partial failure commits what it can in order, reports the durable
// partial outcome, and a live dialog is restored to exactly the committed
// values.
func TestSettingsSavePartialFailureIsDurableAndHonest(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settingsCard().State().Field = uisettings.FieldCoordination
	m.cycleSetting(1)
	if !m.settingsCard().State().Proactive {
		t.Fatal("coordination should be staged proactive")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ThemeAutoSetting, values: map[string]string{uisettings.ThemeSetting: "nord"}}
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}
	rawResult, err := request.execute(writer)
	if err == nil {
		t.Fatal("expected the scripted failure")
	}
	result := rawResult
	if result.outcome.Committed != 2 || result.outcome.Tot != len(request.values) {
		t.Fatalf("committed=%d tot=%d, want 2 of %d", result.outcome.Committed, result.outcome.Tot, len(request.values))
	}
	if got := writer.values[uisettings.ThemeSetting]; got != staged {
		t.Fatalf("committed theme=%q, want %q", got, staged)
	}
	if _, ok := writer.values[uisettings.ThemeAutoSetting]; ok {
		t.Fatal("the failing key was written")
	}
	for _, later := range []string{uisettings.DiffLayoutSetting, uisettings.WorktreeSetting, "coordination"} {
		if _, ok := writer.values[later]; ok {
			t.Fatalf("write after the failure ran: %s", later)
		}
	}

	m.applySettingsEffect(&uieffects.Job{Request: request}, result, err)
	if m.errBar.text == "" {
		t.Fatal("partial failure should surface in the err bar")
	}
	if got := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name; got != staged {
		t.Fatalf("live dialog restored to %q, want committed %q", got, staged)
	}
	if m.settingsCard().State().Proactive {
		t.Fatal("live dialog kept the uncommitted proactive choice")
	}
}

// A failed committed-value read must not be presented as the committed
// value: that key keeps its pre-save state and the failure is reported.
func TestSettingsSaveReadFailureDoesNotFabricateRestoration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	if !m.settingsCard().State().ComfortableRows {
		t.Fatal("density should stage comfortable")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ThemeAutoSetting, failGetOn: uisettings.ListDensitySetting, values: map[string]string{}}
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}
	rawResult, err := request.execute(writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult
	var readErr error
	for _, value := range result.outcome.Restored {
		if value.Key == uisettings.ListDensitySetting {
			readErr = value.Err
		}
	}
	if readErr == nil {
		t.Fatal("the failing read must carry its error in the result")
	}

	m.applySettingsEffect(&uieffects.Job{Request: request}, result, err)
	if !m.settingsCard().State().ComfortableRows {
		t.Fatal("a failed read was fabricated into the committed value")
	}
	if !strings.Contains(m.errBar.text, "reads failed") {
		t.Fatalf("read failures must be reported, err=%q", m.errBar.text)
	}
}

// Partial failure reconciles the live-session prefs as well as the dialog,
// so the runtime shows what persisted.
func TestSettingsSavePartialFailureReconcilesPrefs(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs(m.settingsCard().State()) // the dispatch-time optimistic mirror
	if !m.prefs.comfortableRows {
		t.Fatal("prefs should stage comfortable before persistence")
	}

	writer := &settingsScriptWriter{failOn: uisettings.ListDensitySetting, values: map[string]string{}}
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}
	rawResult, err := request.execute(writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult
	m.applySettingsEffect(&uieffects.Job{Request: request}, result, err)
	if m.prefs.comfortableRows {
		t.Fatal("prefs kept the uncommitted density after a partial save")
	}
	if m.settingsCard().State().ComfortableRows {
		t.Fatal("dialog kept the uncommitted density after a partial save")
	}
}

// A stale completion must not touch the newer dialog's staged state, but
// it does reconcile the runtime prefs to the committed read-back.
func TestSettingsSaveStaleCompletionReconcilesPrefsNotDialog(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}

	m.settings.Gen++
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1) // the newer dialog toggles back to compact
	m.applySettingsPrefs(m.settingsCard().State())
	if m.prefs.comfortableRows {
		t.Fatal("the newer save's preview should be compact")
	}

	result := settingsEffectResult{outcome: uipersist.SaveResult{Generation: request.generation, Restored: []uisettings.Readback{
		{Key: uisettings.ListDensitySetting, Value: "comfortable"},
	}}}
	m.applySettingsEffect(&uieffects.Job{Request: request}, result, errors.New("partial"))
	if !m.prefs.comfortableRows {
		t.Fatal("stale completion must reconcile the runtime prefs to committed")
	}
	if m.settingsCard().State().ComfortableRows {
		t.Fatal("stale completion overwrote the newer dialog's staged state")
	}
	if m.errBar.text == "" {
		t.Fatal("the durable outcome should still be reported")
	}
}

// End to end: a full save fails on a locked store; while it is in flight
// the same dialog is reopened and a hidden-only save is queued behind it.
// The stale failure completion must reconcile the runtime prefs to the
// committed state without overwriting the newer dialog, and a single
// generation must not hide the earlier failure's applied preference.
func TestFailedFullSaveReconcilesPrefsPastStaleGeneration(t *testing.T) {
	m := buildModel(t)
	dir := t.TempDir()
	locked, err := store.Open(filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locked.Close() })
	m.services.store = locked

	locker, err := sql.Open("sqlite", filepath.Join(dir, "state.db")+"?_pragma=busy_timeout(100)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locker.Close() })

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs(m.settingsCard().State()) // the dispatch-time live preview
	if !m.prefs.comfortableRows {
		t.Fatal("dispatch must preview the staged density")
	}
	m.settings.Gen++
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}
	m.enqueueEffect(request, 0, false)
	command := m.nextEffectCmd()

	// The worker's writes now hit a locked database; its reads keep
	// working, so the committed read-back is authoritative.
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	completed := make(chan uieffects.Completed, 1)
	go func() { completed <- command().(uieffects.Completed) }()

	// While the failed save is in flight, the same dialog reopens and a
	// hidden-only save is queued behind it.
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldMouse
	m.cycleSetting(1)
	m.captureHiddenSave(m.settingsCard().State())

	msg := <-completed
	if msg.Err == nil {
		t.Fatal("expected the full save to fail on the locked store")
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
	if m.prefs.comfortableRows {
		t.Fatal("stale completion left the failed save's previewed density applied")
	}
	if !m.settingsCard().State().MouseDisabled {
		t.Fatal("stale completion overwrote the newer dialog's staged choice")
	}
	if m.errBar.text == "" {
		t.Fatal("the failed save must surface in the err bar")
	}
}

// An empty committed hidden state clears the dialog's map; only a failed
// hidden read leaves the old map in place.
func TestSettingsSaveEmptyHiddenClearsDialog(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().CLIHidden = map[string]bool{"claude": true}

	writer := &settingsScriptWriter{failOn: uisettings.HiddenToolsSetting, values: map[string]string{}}
	request := settingsRequest{values: m.settingsCard().State().Values(), hidden: []string{}, generation: m.settings.Gen}
	rawResult, err := request.execute(writer)
	if err == nil {
		t.Fatal("expected the hidden write to fail")
	}
	result := rawResult
	if result.outcome.HiddenErr != nil {
		t.Fatalf("hidden read failed: %v", result.outcome.HiddenErr)
	}
	m.applySettingsEffect(&uieffects.Job{Request: request}, result, err)
	if len(m.settingsCard().State().CLIHidden) != 0 {
		t.Fatalf("committed empty hidden state did not clear the dialog: %v", m.settingsCard().State().CLIHidden)
	}

	// A failed hidden read keeps the old map.
	writer.failGetOn = uisettings.HiddenToolsSetting
	request2 := settingsRequest{values: m.settingsCard().State().Values(), hidden: []string{}, generation: m.settings.Gen}
	rawResult2, err2 := request2.execute(writer)
	result2 := rawResult2
	if err2 == nil || result2.outcome.HiddenErr == nil {
		t.Fatal("expected a failed hidden read")
	}
	m.settingsCard().State().CLIHidden = map[string]bool{"codex": true}
	m.applySettingsEffect(&uieffects.Job{Request: request2}, result2, err2)
	if !m.settingsCard().State().CLIHidden["codex"] {
		t.Fatal("failed hidden read must keep the dialog's map")
	}
}

// A completion whose generation is older than the latest accepted save
// only reports; it never overwrites the newer dialog's state.
func TestSettingsSaveCompletionRefusesStaleGeneration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}

	m.settings.Gen++
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	newer := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name

	result := settingsEffectResult{outcome: uipersist.SaveResult{Generation: request.generation, Restored: []uisettings.Readback{{Key: uisettings.ThemeSetting, Value: "nord"}}}}
	m.applySettingsEffect(&uieffects.Job{Request: request}, result, errors.New("partial"))
	if uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name != newer {
		t.Fatal("stale completion overwrote the newer dialog")
	}
	if m.errBar.text == "" {
		t.Fatal("the durable outcome should still be reported")
	}
}

// While the store is locked, the window keeps answering; the accepted
// save lands once the writer is free.
func TestSettingsSaveDoesNotBlockWindowUpdate(t *testing.T) {
	m := buildModel(t)
	dbPath := filepath.Join(t.TempDir(), "locked.db")
	locked, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locked.Close() })
	m.services.store = locked

	locker, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(100)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { locker.Close() })
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}

	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	_, command := m.handleSettingsKey(key("enter"))

	completed := make(chan tea.Msg, 1)
	go func() { completed <- command() }()
	time.Sleep(300 * time.Millisecond) // the worker is now parked on the lock
	updated := make(chan struct{})
	go func() { m.Update(tea.WindowSizeMsg{Width: 150, Height: 45}); close(updated) }()
	select {
	case <-updated:
	case <-time.After(time.Second):
		locker.Exec("COMMIT")
		<-completed
		t.Fatal("window update waited on the blocked settings writer")
	}
	if _, err := locker.Exec("COMMIT"); err != nil {
		t.Fatal(err)
	}
	msg := <-completed
	m.applyTestMsg(t, msg)
	m.drainEffects(t)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want %q", got, staged)
	}
}

// An accepted settings save is not discarded by quit; it drains FIFO.
func TestEffectQueueDrainsSettingsSaveOnQuit(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settings.Gen++
	request := settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}
	m.enqueueEffect(request, 0, false)

	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded the accepted save")
	}
	m.drainEffects(t)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("FIFO final theme=%s, want %s", got, staged)
	}
}

// Two hidden saves accepted back to back each commit; the older
// completion must not undo the newer one's state.
func TestCLIPickerSavesFenceEachOther(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 0
	first := m.settingsCard().State().CLINames[0]
	m.handleSettingsKey(key(" "))
	_, cmd1 := m.handleSettingsKey(key("esc"))
	if cmd1 == nil {
		t.Fatal("first save accepted no command")
	}
	m.openSettingsCLIPicker(t)
	m.settingsCard().State().CLICursor = 1
	second := m.settingsCard().State().CLINames[1]
	m.handleSettingsKey(key(" "))
	m.handleSettingsKey(key("esc")) // enqueues the second save; its command dispatches after the first

	m.applyTestMsg(t, cmd1())
	m.drainEffects(t)
	raw, err := m.services.store.Setting(uisettings.HiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != first+","+second {
		t.Fatalf("stored hidden_tools=%q, want %q", raw, first+","+second)
	}
}

// Duplicate commands and completions write once.
func TestSettingsSaveDuplicateCompletionIsIdempotent(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().Field = uisettings.FieldTheme
	m.cycleSetting(1)
	staged := uitheme.Palettes[m.settingsCard().State().ThemeIndex].Name
	m.settings.Gen++
	m.enqueueEffect(settingsRequest{values: m.settingsCard().State().Values(), generation: m.settings.Gen}, 0, false)
	command := m.nextEffectCmd()
	msg := command()
	command()
	m.applyTestMsg(t, msg)
	m.applyTestMsg(t, msg)
	got, err := m.services.store.Setting(uisettings.ThemeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != staged {
		t.Fatalf("stored theme=%q, want %q", got, staged)
	}
}

// The request keeps its own copies of the mutable dialog slices.
func TestSettingsRequestCopiesMutableValues(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settingsCard().State().CLIHidden = map[string]bool{"claude": true}
	request := settingsRequest{values: m.settingsCard().State().Values(), hidden: m.settingsCard().State().HiddenList(), generation: 1}
	m.enqueueEffect(request, 0, false)
	request.values[0].Value = "changed"
	request.hidden[0] = "changed"
	stored := m.effects.Main.Pending[0].Request.(settingsRequest)
	if stored.values[0].Value == "changed" || stored.hidden[0] == "changed" {
		t.Fatal("request retained mutable dialog aliases")
	}
}

func TestLaterSuccessfulSettingsSaveReappliesCommittedPrefs(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	request := settingsRequest{values: []uisettings.Value{{Key: uisettings.ListDensitySetting, Value: "comfortable"}}}
	m.prefs.comfortableRows = false
	m.applySettingsEffect(&uieffects.Job{Request: request}, settingsEffectResult{}, nil)
	if !m.prefs.comfortableRows {
		t.Fatal("successful queued save did not reconcile its committed preferences")
	}
}

func TestFailedSettingsSavePreservesUnsetDefaults(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	if !m.prefs.focusOnEnter || !m.prefs.arrowStep {
		t.Fatal("fixture does not start with enabled defaults")
	}
	m.settingsCard().State().Field = uisettings.FieldDensity
	m.cycleSetting(1)
	_, _ = m.closeSettings()
	job := m.effects.Main.Active
	request := job.Request.(settingsRequest)
	writer := &settingsScriptWriter{failOn: uisettings.ListDensitySetting, values: map[string]string{}}
	result, err := request.execute(writer)
	if err == nil {
		t.Fatal("expected failed save")
	}
	m.applySettingsEffect(job, result, err)
	if !m.prefs.focusOnEnter || !m.prefs.arrowStep {
		t.Fatalf("failed density save changed unrelated defaults, focus=%v arrows=%v", m.prefs.focusOnEnter, m.prefs.arrowStep)
	}
}
