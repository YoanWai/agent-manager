package ui

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// settingsScriptWriter scripts failing writes and reads so the partial
// outcome can be proven deterministically.
type settingsScriptWriter struct {
	failOn    string
	failGetOn string
	values    map[string]string
}

type blockedSettingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
	values  map[string]string
}

func (r *blockedSettingReader) get(key string) (string, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.values[key], nil
}

func TestOpenSettingsDefersBlockedStoreRead(t *testing.T) {
	m := buildModel(t)
	reader := &blockedSettingReader{
		started: make(chan struct{}),
		release: make(chan struct{}),
		values:  map[string]string{worktreeSetting: "on"},
	}
	opened := make(chan tea.Cmd, 1)
	go func() { opened <- m.openSettingsWithReader(reader) }()

	var cmd tea.Cmd
	select {
	case <-reader.started:
		close(reader.release)
		<-opened
		t.Fatal("opening settings read the store on the update path")
	case cmd = <-opened:
	}
	if cmd == nil {
		t.Fatal("opening settings did not return the deferred refresh")
	}
	select {
	case <-reader.started:
		t.Fatal("settings reader ran before its Bubble Tea command")
	default:
	}

	// Opening batches the store read with the editor probe; each runs as
	// its own command, so the blocked read holds only itself.
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("opening settings should batch the store read with the editor probe")
	}
	completed := make(chan tea.Msg, len(batch))
	for _, each := range batch {
		go func() { completed <- each() }()
	}
	<-reader.started
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 151, Height: 45})
	m = updated.(*Model)
	if m.layout.width != 151 {
		t.Fatal("a blocked settings reader blocked an unrelated window update")
	}
	close(reader.release)
	for range batch {
		m.applyTestMsg(t, <-completed)
	}
	if !m.settings.dialog.worktreeDefault {
		t.Fatal("the accepted deferred refresh did not update the dialog")
	}
}

func TestOpenCLIPickerDoesNotReadStore(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.Close(); err != nil {
		t.Fatal(err)
	}
	m.errBar.text = ""
	m.settings.dialog.cliHidden = nil
	m.openCLIPicker()
	if m.errBar.text != "" {
		t.Fatalf("opening the CLI picker read the closed store: %q", m.errBar.text)
	}
}

func TestSettingsOpenRefreshesExternalChanges(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(hiddenToolsSetting, "claude"); err != nil {
		t.Fatal(err)
	}

	cmd := m.openSettings()
	if m.settings.dialog.worktreeDefault || m.settings.dialog.cliHidden["claude"] {
		t.Fatal("opening should paint the cached preferences before the refresh")
	}
	m.applyTestMsg(t, cmd())
	if !m.settings.dialog.worktreeDefault || !m.settings.dialog.cliHidden["claude"] {
		t.Fatalf("external settings were not refreshed: worktree=%t hidden=%v", m.settings.dialog.worktreeDefault, m.settings.dialog.cliHidden)
	}
}

func TestSettingsLoadRefusesReopenedDialog(t *testing.T) {
	m := buildModel(t)
	cmd := m.openSettings()
	firstGeneration := m.settings.gen
	if err := m.services.store.SetSetting(worktreeSetting, "on"); err != nil {
		t.Fatal(err)
	}
	loaded := cmd()

	m.openSettings()
	if m.settings.gen == firstGeneration {
		t.Fatal("reopening settings reused the old generation")
	}
	m.applyTestMsg(t, loaded)
	if m.settings.dialog.worktreeDefault {
		t.Fatal("the first dialog's load overwrote the reopened dialog")
	}
}

func TestSettingsLoadRefusesEditsAndSubpickers(t *testing.T) {
	t.Run("staged edit", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settings.dialog.field = settingsFieldWorktree
		m.cycleSetting(1)
		m.applyTestMsg(t, cmd())
		if !m.settings.dialog.worktreeDefault {
			t.Fatal("the async load overwrote a staged setting")
		}
	})

	t.Run("CLI picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		if err := m.services.store.SetSetting(hiddenToolsSetting, "claude"); err != nil {
			t.Fatal(err)
		}
		m.openCLIPicker()
		m.applyTestMsg(t, cmd())
		if m.settings.dialog.cliHidden["claude"] {
			t.Fatal("the async load replaced an open CLI picker")
		}
	})

	t.Run("key picker", func(t *testing.T) {
		m := buildModel(t)
		cmd := m.openSettings()
		m.settings.dialog.keyPicker = true
		before := m.settings.dialog.worktreeDefault
		if err := m.services.store.SetSetting(worktreeSetting, "on"); err != nil {
			t.Fatal(err)
		}
		m.applyTestMsg(t, cmd())
		if m.settings.dialog.worktreeDefault != before {
			t.Fatal("the async load replaced an open key picker")
		}
	})
}

func TestSettingsReopenUsesOptimisticPendingSave(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settings.dialog.field = settingsFieldWorktree
	m.cycleSetting(1)
	_, save := m.handleSettingsKey(key("enter"))
	if save == nil {
		t.Fatal("settings save was not queued")
	}

	// Only the editor PATH probe may run; a store load would race the save.
	if refresh := m.openSettings(); refresh != nil {
		if _, probe := refresh().(editorsProbedMsg); !probe {
			t.Fatal("a dialog reopened over a pending save should use the optimistic cache")
		}
	}
	if !m.settings.dialog.worktreeDefault {
		t.Fatal("the reopened dialog lost the pending worktree choice")
	}
	m.applyCmd(t, save)
}

func (w *settingsScriptWriter) set(key, value string) error {
	if w.failOn == key {
		return errors.New("scripted write failure")
	}
	w.values[key] = value
	return nil
}

func (w *settingsScriptWriter) setProactive(proactive bool) error {
	if w.failOn == "coordination" {
		return errors.New("scripted write failure")
	}
	w.values["coordination"] = "proactive"
	return nil
}

func (w *settingsScriptWriter) get(key string) (string, error) {
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	staged := themes[m.settings.dialog.themeIndex].Name

	_, cmd := m.handleSettingsKey(key("enter"))
	if m.mode != modeList {
		t.Fatalf("enter should close the dialog, mode=%v", m.mode)
	}
	got, err := m.services.store.Setting(themeSetting)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("theme written on the update path: %q", got)
	}
	m.applyCmd(t, cmd)
	got, err = m.services.store.Setting(themeSetting)
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
	m.openCLIPicker()
	m.settings.dialog.cliCursor = 1
	name := m.settings.dialog.cliNames[1]
	m.handleSettingsKey(key(" "))
	if !m.settings.dialog.cliHidden[name] {
		t.Fatal("space should hide the focused CLI")
	}
	_, cmd := m.handleSettingsKey(key("esc"))
	if m.settings.dialog.cliPicker {
		t.Fatal("esc should leave the picker")
	}
	raw, err := m.services.store.Setting(hiddenToolsSetting)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "" {
		t.Fatalf("hidden_tools written on the update path: %q", raw)
	}
	m.applyCmd(t, cmd)
	raw, err = m.services.store.Setting(hiddenToolsSetting)
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	captured := themes[m.settings.dialog.themeIndex].Name
	_, cmd := m.handleSettingsKey(key("enter"))

	m.openSettings()
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	m.cycleSetting(1)
	m.applyCmd(t, cmd)

	got, err := m.services.store.Setting(themeSetting)
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	staged := themes[m.settings.dialog.themeIndex].Name
	m.settings.dialog.field = settingsFieldCoordination
	m.cycleSetting(1)
	if !m.settings.dialog.proactive {
		t.Fatal("coordination should be staged proactive")
	}

	writer := &settingsScriptWriter{failOn: themeAutoSetting, values: map[string]string{themeSetting: "nord"}}
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}
	rawResult, err := runSettingsWithWriter(request, writer)
	if err == nil {
		t.Fatal("expected the scripted failure")
	}
	result := rawResult.(settingsEffectResult)
	if result.committed != 2 || result.tot != len(request.values) {
		t.Fatalf("committed=%d tot=%d, want 2 of %d", result.committed, result.tot, len(request.values))
	}
	if got := writer.values[themeSetting]; got != staged {
		t.Fatalf("committed theme=%q, want %q", got, staged)
	}
	if _, ok := writer.values[themeAutoSetting]; ok {
		t.Fatal("the failing key was written")
	}
	for _, later := range []string{diffLayoutSetting, worktreeSetting, "coordination"} {
		if _, ok := writer.values[later]; ok {
			t.Fatalf("write after the failure ran: %s", later)
		}
	}

	m.applySettingsEffect(&effectJob{request: request}, result, err)
	if m.errBar.text == "" {
		t.Fatal("partial failure should surface in the err bar")
	}
	if got := themes[m.settings.dialog.themeIndex].Name; got != staged {
		t.Fatalf("live dialog restored to %q, want committed %q", got, staged)
	}
	if m.settings.dialog.proactive {
		t.Fatal("live dialog kept the uncommitted proactive choice")
	}
}

// A failed committed-value read must not be presented as the committed
// value: that key keeps its pre-save state and the failure is reported.
func TestSettingsSaveReadFailureDoesNotFabricateRestoration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settings.dialog.field = settingsFieldDensity
	m.cycleSetting(1)
	if !m.settings.dialog.comfortableRows {
		t.Fatal("density should stage comfortable")
	}

	writer := &settingsScriptWriter{failOn: themeAutoSetting, failGetOn: listDensitySetting, values: map[string]string{}}
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}
	rawResult, err := runSettingsWithWriter(request, writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult.(settingsEffectResult)
	var readErr error
	for _, value := range result.restored {
		if value.key == listDensitySetting {
			readErr = value.err
		}
	}
	if readErr == nil {
		t.Fatal("the failing read must carry its error in the result")
	}

	m.applySettingsEffect(&effectJob{request: request}, result, err)
	if !m.settings.dialog.comfortableRows {
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
	m.settings.dialog.field = settingsFieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs() // the dispatch-time optimistic mirror
	if !m.prefs.comfortableRows {
		t.Fatal("prefs should stage comfortable before persistence")
	}

	writer := &settingsScriptWriter{failOn: listDensitySetting, values: map[string]string{}}
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}
	rawResult, err := runSettingsWithWriter(request, writer)
	if err == nil {
		t.Fatal("expected the scripted write failure")
	}
	result := rawResult.(settingsEffectResult)
	m.applySettingsEffect(&effectJob{request: request}, result, err)
	if m.prefs.comfortableRows {
		t.Fatal("prefs kept the uncommitted density after a partial save")
	}
	if m.settings.dialog.comfortableRows {
		t.Fatal("dialog kept the uncommitted density after a partial save")
	}
}

// A stale completion must not touch the newer dialog's staged state, but
// it does reconcile the runtime prefs to the committed read-back.
func TestSettingsSaveStaleCompletionReconcilesPrefsNotDialog(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settings.dialog.field = settingsFieldDensity
	m.cycleSetting(1)
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}

	m.settings.gen++
	m.settings.dialog.field = settingsFieldDensity
	m.cycleSetting(1) // the newer dialog toggles back to compact
	m.applySettingsPrefs()
	if m.prefs.comfortableRows {
		t.Fatal("the newer save's preview should be compact")
	}

	result := settingsEffectResult{generation: request.generation, restored: []restoredValue{
		{key: listDensitySetting, value: "comfortable"},
	}}
	m.applySettingsEffect(&effectJob{request: request}, result, errors.New("partial"))
	if !m.prefs.comfortableRows {
		t.Fatal("stale completion must reconcile the runtime prefs to committed")
	}
	if m.settings.dialog.comfortableRows {
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
	m.settings.dialog.field = settingsFieldDensity
	m.cycleSetting(1)
	m.applySettingsPrefs() // the dispatch-time live preview
	if !m.prefs.comfortableRows {
		t.Fatal("dispatch must preview the staged density")
	}
	m.settings.gen++
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}
	m.enqueueEffect(request, 0, false)
	command := m.nextEffectCmd()

	// The worker's writes now hit a locked database; its reads keep
	// working, so the committed read-back is authoritative.
	if _, err := locker.Exec("BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	completed := make(chan effectCompletedMsg, 1)
	go func() { completed <- command().(effectCompletedMsg) }()

	// While the failed save is in flight, the same dialog reopens and a
	// hidden-only save is queued behind it.
	m.openSettings()
	m.settings.dialog.field = settingsFieldMouse
	m.cycleSetting(1)
	m.captureHiddenSave()

	msg := <-completed
	if msg.err == nil {
		t.Fatal("expected the full save to fail on the locked store")
	}
	updated, _ := m.Update(msg)
	*m = *updated.(*Model)
	if m.prefs.comfortableRows {
		t.Fatal("stale completion left the failed save's previewed density applied")
	}
	if !m.settings.dialog.mouseDisabled {
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
	m.settings.dialog.cliHidden = map[string]bool{"claude": true}

	writer := &settingsScriptWriter{failOn: hiddenToolsSetting, values: map[string]string{}}
	request := settingsRequest{values: m.captureSettingValues(), hidden: []string{}, generation: m.settings.gen}
	rawResult, err := runSettingsWithWriter(request, writer)
	if err == nil {
		t.Fatal("expected the hidden write to fail")
	}
	result := rawResult.(settingsEffectResult)
	if result.hiddenErr != nil {
		t.Fatalf("hidden read failed: %v", result.hiddenErr)
	}
	m.applySettingsEffect(&effectJob{request: request}, result, err)
	if len(m.settings.dialog.cliHidden) != 0 {
		t.Fatalf("committed empty hidden state did not clear the dialog: %v", m.settings.dialog.cliHidden)
	}

	// A failed hidden read keeps the old map.
	writer.failGetOn = hiddenToolsSetting
	request2 := settingsRequest{values: m.captureSettingValues(), hidden: []string{}, generation: m.settings.gen}
	rawResult2, err2 := runSettingsWithWriter(request2, writer)
	result2 := rawResult2.(settingsEffectResult)
	if err2 == nil || result2.hiddenErr == nil {
		t.Fatal("expected a failed hidden read")
	}
	m.settings.dialog.cliHidden = map[string]bool{"codex": true}
	m.applySettingsEffect(&effectJob{request: request2}, result2, err2)
	if !m.settings.dialog.cliHidden["codex"] {
		t.Fatal("failed hidden read must keep the dialog's map")
	}
}

// A completion whose generation is older than the latest accepted save
// only reports; it never overwrites the newer dialog's state.
func TestSettingsSaveCompletionRefusesStaleGeneration(t *testing.T) {
	m := buildModel(t)
	m.openSettings()
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}

	m.settings.gen++
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	newer := themes[m.settings.dialog.themeIndex].Name

	result := settingsEffectResult{generation: request.generation, restored: []restoredValue{{key: themeSetting, value: "nord"}}}
	m.applySettingsEffect(&effectJob{request: request}, result, errors.New("partial"))
	if themes[m.settings.dialog.themeIndex].Name != newer {
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	staged := themes[m.settings.dialog.themeIndex].Name
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
	got, err := m.services.store.Setting(themeSetting)
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	staged := themes[m.settings.dialog.themeIndex].Name
	m.settings.gen++
	request := settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}
	m.enqueueEffect(request, 0, false)

	_, command := m.requestQuit()
	if _, ok := command().(tea.QuitMsg); ok {
		t.Fatal("quit discarded the accepted save")
	}
	m.drainEffects(t)
	got, err := m.services.store.Setting(themeSetting)
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
	m.openCLIPicker()
	m.settings.dialog.cliCursor = 0
	first := m.settings.dialog.cliNames[0]
	m.handleSettingsKey(key(" "))
	_, cmd1 := m.handleSettingsKey(key("esc"))
	if cmd1 == nil {
		t.Fatal("first save accepted no command")
	}
	m.openCLIPicker()
	m.settings.dialog.cliCursor = 1
	second := m.settings.dialog.cliNames[1]
	m.handleSettingsKey(key(" "))
	m.handleSettingsKey(key("esc")) // enqueues the second save; its command dispatches after the first

	m.applyTestMsg(t, cmd1())
	m.drainEffects(t)
	raw, err := m.services.store.Setting(hiddenToolsSetting)
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
	m.settings.dialog.field = settingsFieldTheme
	m.cycleSetting(1)
	staged := themes[m.settings.dialog.themeIndex].Name
	m.settings.gen++
	m.enqueueEffect(settingsRequest{values: m.captureSettingValues(), generation: m.settings.gen}, 0, false)
	command := m.nextEffectCmd()
	msg := command()
	command()
	m.applyTestMsg(t, msg)
	m.applyTestMsg(t, msg)
	got, err := m.services.store.Setting(themeSetting)
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
	m.settings.dialog.cliHidden = map[string]bool{"claude": true}
	request := settingsRequest{values: m.captureSettingValues(), hidden: m.hiddenToolList(), generation: 1}
	m.enqueueEffect(request, 0, false)
	request.values[0].value = "changed"
	request.hidden[0] = "changed"
	stored := m.effects.pending[0].request.(settingsRequest)
	if stored.values[0].value == "changed" || stored.hidden[0] == "changed" {
		t.Fatal("request retained mutable dialog aliases")
	}
}

func TestLaterSuccessfulSettingsSaveReappliesCommittedPrefs(t *testing.T) {
	m := buildModel(t)
	request := settingsRequest{values: []settingValue{{key: listDensitySetting, value: "comfortable"}}}
	m.prefs.comfortableRows = false
	m.applySettingsEffect(&effectJob{request: request}, settingsEffectResult{}, nil)
	if !m.prefs.comfortableRows {
		t.Fatal("successful queued save did not reconcile its committed preferences")
	}
}
