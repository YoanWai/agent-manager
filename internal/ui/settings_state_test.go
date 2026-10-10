package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeSettingsHost is everything the Settings feature reaches in the root,
// with no tmux, store or root model behind it.
type fakeSettingsHost struct {
	cfg             config.Config
	session, list   keybind.Table
	queued          []keysRequest
	submitted       []effectRequest
	err             string
	paneSyncs       int
	background      *bool
	version, latest string
	applying        bool
	height          int
}

func (h *fakeSettingsHost) reportErr(text string)     { h.err = text }
func (h *fakeSettingsHost) clearErr()                 { h.err = "" }
func (h *fakeSettingsHost) toolConfig() config.Config { return h.cfg }
func (h *fakeSettingsHost) keyTables() (session, list keybind.Table) {
	return h.session, h.list
}
func (h *fakeSettingsHost) queuedKeys() []keysRequest { return h.queued }
func (h *fakeSettingsHost) submitSettings(request settingsRequest) tea.Cmd {
	h.submitted = append(h.submitted, request)
	return nil
}
func (h *fakeSettingsHost) submitKeys(request keysRequest) tea.Cmd {
	h.submitted = append(h.submitted, request)
	return nil
}
func (h *fakeSettingsHost) syncPaneTheme() tea.Cmd { h.paneSyncs++; return nil }
func (h *fakeSettingsHost) previewBackground(terminal bool) {
	h.background = &terminal
}
func (h *fakeSettingsHost) release() (version, latest string, applying bool) {
	return h.version, h.latest, h.applying
}
func (h *fakeSettingsHost) dialogHeight() int { return h.height }
func (h *fakeSettingsHost) card(title, body string, _ [][2]string) string {
	return title + "\n" + body
}
func (h *fakeSettingsHost) cardFlex(title, body string, _ [][2]string) string {
	return title + "\n" + body
}
func (h *fakeSettingsHost) confirmCard(title, question, consequence string, _ bool, _ string) string {
	return title + "\n" + question + "\n" + consequence
}

var (
	_ settingsHost     = (*Model)(nil)
	_ settingsViewHost = (*Model)(nil)
)

type mapSettingReader map[string]string

func (r mapSettingReader) get(key string) (string, error) { return r[key], nil }

func newFakeSettingsHost() *fakeSettingsHost {
	return &fakeSettingsHost{
		cfg:     config.Config{Tools: map[string]config.Tool{"claude": {}, "codex": {}}},
		session: keybind.DefaultSession(),
		list:    keybind.DefaultList(),
		version: "v1.0.0",
		height:  40,
	}
}

func pressSettings(t *testing.T, s *settingsFeature, h settingsHost, keys ...string) (tea.Cmd, settingsExit) {
	t.Helper()
	var cmd tea.Cmd
	exit := settingsStay
	for _, k := range keys {
		// The root adapter routes an open key picker the same way.
		if s.dialog.keyPicker && !s.dialog.cliPicker {
			cmd, exit = s.handleKeyPickerKey(h.(keyPickerHost), key(k)), settingsStay
			continue
		}
		cmd, exit = s.handleKey(h, key(k))
	}
	return cmd, exit
}

func focusSettingsField(s *settingsFeature, field int) {
	s.dialog.field = field
}

// The feature opens, steps, and asks to close through its host alone.
func TestSettingsFeatureDrivesThroughNarrowHost(t *testing.T) {
	h := newFakeSettingsHost()
	var s settingsFeature
	cmd, opened := s.open(h, mapSettingReader{})
	if !opened || cmd == nil {
		t.Fatalf("open = %v, %v; want the load and probe commands", cmd, opened)
	}
	if got := strings.Join(s.dialog.toolNames, ","); !strings.Contains(got, "claude") || !strings.Contains(got, "codex") {
		t.Fatalf("tool names %q do not come from the host's config", got)
	}

	focusSettingsField(&s, settingsFieldBackground)
	pressSettings(t, &s, h, "right")
	if h.background == nil || !*h.background || !s.dialog.terminalBackground || !s.dialog.dirty {
		t.Fatalf("background step did not preview through the host: %+v", h.background)
	}

	focusSettingsField(&s, settingsFieldUpdate)
	if _, exit := pressSettings(t, &s, h, "enter"); exit != settingsUpdate {
		t.Fatalf("enter on the version row = %v, want settingsUpdate", exit)
	}
	focusSettingsField(&s, settingsFieldBugReport)
	if _, exit := pressSettings(t, &s, h, "enter"); exit != settingsReportBug {
		t.Fatalf("enter on report a bug = %v, want settingsReportBug", exit)
	}
	if _, exit := pressSettings(t, &s, h, "esc"); exit != settingsSave {
		t.Fatalf("esc = %v, want settingsSave", exit)
	}

	s.captureSettingsSave(h, true, false)
	if len(h.submitted) != 1 || s.pending != 1 {
		t.Fatalf("save submitted %d requests with %d pending, want 1 and 1", len(h.submitted), s.pending)
	}
	request, ok := h.submitted[0].(settingsRequest)
	if !ok || request.generation != s.gen || s.cache.value(backgroundSetting) != "terminal" {
		t.Fatalf("submitted %#v; want a fenced settings request staged in the cache", h.submitted[0])
	}
	if len(request.values) != 1 || request.values[0].key != backgroundSetting || request.hidden != nil {
		t.Fatalf("submitted %#v; want only the changed background key", request)
	}
}

// The CLI picker keeps one CLI enabled and saves the hidden set on esc.
func TestSettingsFeatureCLIPickerThroughHost(t *testing.T) {
	h := newFakeSettingsHost()
	var s settingsFeature
	s.open(h, mapSettingReader{})
	focusSettingsField(&s, settingsFieldCLIs)
	pressSettings(t, &s, h, "enter")
	if !s.dialog.cliPicker || len(s.dialog.cliNames) != 2 {
		t.Fatalf("CLI picker = %v with %v", s.dialog.cliPicker, s.dialog.cliNames)
	}
	pressSettings(t, &s, h, " ", "down", " ")
	if h.err != "keep at least one CLI enabled" {
		t.Fatalf("hiding the last CLI reported %q", h.err)
	}
	pressSettings(t, &s, h, "esc")
	if len(h.submitted) != 1 {
		t.Fatalf("esc submitted %d requests, want the hidden save", len(h.submitted))
	}
	request := h.submitted[0].(settingsRequest)
	if len(request.hidden) != 1 || request.hidden[0] != s.dialog.cliNames[0] || request.values != nil {
		t.Fatalf("hidden save = %#v", request)
	}
}

// The key picker reads the live tables from the host and submits a change
// only when it differs from what the lane will already commit.
func TestSettingsFeatureKeyPickerThroughHost(t *testing.T) {
	h := newFakeSettingsHost()
	var s settingsFeature
	s.open(h, mapSettingReader{})
	focusSettingsField(&s, settingsFieldKeybindings)
	pressSettings(t, &s, h, "enter")
	if !s.dialog.keyPicker || !s.dialog.tables[0].Equal(h.session) || !s.dialog.tables[1].Equal(h.list) {
		t.Fatal("key picker did not open on the host's tables")
	}
	view := s.view(h)
	if !strings.Contains(view, "⚙ Keybindings") || !strings.Contains(view, s.pickedRow().action.Name) {
		t.Fatalf("key picker view:\n%s", view)
	}
	pressSettings(t, &s, h, "down", "d", "esc")
	if len(h.submitted) != 1 {
		t.Fatalf("esc submitted %d requests, want one keys request", len(h.submitted))
	}
	changed := h.submitted[0].(keysRequest)
	if !changed.sessionChanged || changed.listChanged {
		t.Fatalf("keys request = %#v, want only the session table", changed)
	}

	h.queued, h.submitted = []keysRequest{changed}, nil
	s.dialog.keyPicker = true
	pressSettings(t, &s, h, "esc")
	if len(h.submitted) != 0 {
		t.Fatalf("a table the lane already commits was submitted again: %#v", h.submitted)
	}
}

// The dialog paints from its own state plus the host's release facts.
func TestSettingsFeatureViewReadsHostFacts(t *testing.T) {
	h := newFakeSettingsHost()
	h.latest = "v2.0.0"
	var s settingsFeature
	s.open(h, mapSettingReader{})
	view := s.view(h)
	if !strings.Contains(view, "version v1.0.0") || !strings.Contains(view, "update to v2.0.0") {
		t.Fatalf("version row did not read the host's release:\n%s", view)
	}
	if !strings.Contains(view, "keybindings") || !strings.Contains(view, "defaults") {
		t.Fatalf("keybindings row did not summarise the host's tables:\n%s", view)
	}
}
