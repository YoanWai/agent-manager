package ui

import (
	"context"
	"github.com/YoanWai/agent-manager/internal/systheme"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAutoThemeName(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		scheme systheme.Scheme
		want   string
	}{
		{"unknown keeps stored", "nord", systheme.SchemeUnknown, "nord"},
		{"dark scheme keeps a dark stored theme", "nord", systheme.SchemeDark, "nord"},
		{"light scheme keeps a light stored theme", "paper", systheme.SchemeLight, "paper"},
		{"light scheme flips a dark stored theme", "nord", systheme.SchemeLight, "solarized light"},
		{"dark scheme flips a light stored theme", "paper", systheme.SchemeDark, "classic"},
		{"unset stored resolves through the default", "", systheme.SchemeLight, "solarized light"},
		{"light scheme flips a dark theme to its counterpart", "catppuccin mocha", systheme.SchemeLight, "catppuccin latte"},
		{"dark scheme flips a light theme to its counterpart", "kanagawa lotus", systheme.SchemeDark, "kanagawa wave"},
		{"dark scheme flips solarized light to solarized dark", "solarized light", systheme.SchemeDark, "solarized dark"},
		{"light scheme keeps a light theme that has a counterpart", "catppuccin latte", systheme.SchemeLight, "catppuccin latte"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := autoThemeName(tt.stored, tt.scheme); got != tt.want {
				t.Errorf("autoThemeName(%q, %v) = %q, want %q", tt.stored, tt.scheme, got, tt.want)
			}
		})
	}
}

func TestThemeAutoPersistsWithoutClobberingManualTheme(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if m.settings.dialog.ThemeAuto {
		t.Fatal("theme auto should default off")
	}
	m.settings.dialog.Field = uisettings.FieldThemeAuto
	m.cycleSetting(1)
	if !m.settings.dialog.ThemeAuto {
		t.Fatal("toggle should enable theme auto")
	}
	m.applyCmd(t, m.captureSettingsSave(true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != "nord" {
		t.Fatalf("manual theme clobbered by auto: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "on" {
		t.Fatalf("theme_auto not persisted: %q", got)
	}
	if !themeAutoEnabled(m.services.store) {
		t.Fatal("themeAutoEnabled should read the persisted toggle")
	}
}

func TestManualThemeCycleDisablesAuto(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "classic"); err != nil {
		t.Fatal(err)
	}
	if err := m.services.store.SetSetting(uisettings.ThemeAutoSetting, "on"); err != nil {
		t.Fatal(err)
	}
	m.applyTestMsg(t, m.openSettings()())
	if !m.settings.dialog.ThemeAuto {
		t.Fatal("open should reflect the persisted auto toggle")
	}
	m.settings.dialog.Field = uisettings.FieldTheme
	m.cycleSetting(1)
	if m.settings.dialog.ThemeAuto {
		t.Fatal("stepping the theme by hand should turn auto off")
	}
	m.applyCmd(t, m.captureSettingsSave(true, false))
	if got, _ := m.services.store.Setting(uisettings.ThemeAutoSetting); got != "off" {
		t.Fatalf("theme_auto should persist off after a manual step: %q", got)
	}
	if got, _ := m.services.store.Setting(uisettings.ThemeSetting); got != themes[m.settings.dialog.ThemeIndex].Name {
		t.Fatalf("manual step not persisted: %q", got)
	}
}

func TestResolveStartupThemeWithoutAuto(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.SetSetting(uisettings.ThemeSetting, "nord"); err != nil {
		t.Fatal(err)
	}
	if got := resolveStartupTheme(m.services.store); got != "nord" {
		t.Fatalf("resolveStartupTheme = %q, want the stored theme", got)
	}
}

type schemeRecorder struct{ got []tea.Msg }

func (r *schemeRecorder) Init() tea.Cmd { return nil }

func (r *schemeRecorder) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if scheme, ok := msg.(colorSchemeMsg); ok {
		r.got = append(r.got, scheme)
		if len(r.got) == 2 {
			return r, tea.Quit
		}
	}
	return r, nil
}

func (r *schemeRecorder) View() string { return "" }

// Reports go through Bubble Tea's real input reader, so a release that
// renames or decodes its unknown-CSI message breaks this test, not users.
func TestColorSchemeFilterTranslatesBubbleTeaInput(t *testing.T) {
	input := strings.NewReader(ansi.LightDarkReport(false) + ansi.LightDarkReport(true))
	recorder := &schemeRecorder{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	program := tea.NewProgram(recorder, tea.WithInput(input), tea.WithOutput(io.Discard),
		tea.WithoutRenderer(), tea.WithContext(ctx), tea.WithFilter(ColorSchemeFilter))
	if _, err := program.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	want := []tea.Msg{colorSchemeMsg{light: true}, colorSchemeMsg{light: false}}
	if !reflect.DeepEqual(recorder.got, want) {
		t.Fatalf("got %v, want %v", recorder.got, want)
	}
}

func TestColorSchemeFilterPassesOtherMessages(t *testing.T) {
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}
	if got := ColorSchemeFilter(nil, key); !reflect.DeepEqual(got, key) {
		t.Fatalf("filter changed %v into %v", key, got)
	}
}

func TestFollowColorScheme(t *testing.T) {
	tests := []struct {
		name  string
		auto  string
		theme string
		light bool
		want  string
	}{
		{"light flips a dark theme to its counterpart", "on", "catppuccin mocha", true, "catppuccin latte"},
		{"dark flips back to the stored theme", "on", "catppuccin mocha", false, "catppuccin mocha"},
		{"auto off keeps the theme", "off", "catppuccin mocha", true, "catppuccin mocha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := buildModel(t)
			t.Cleanup(func() { applyTheme(themes[0]) })
			applyTheme(themes[themeIndex(tt.theme)])
			m.settings.cache.applyValues([]settingValue{
				{Key: uisettings.ThemeAutoSetting, Value: tt.auto},
				{Key: uisettings.ThemeSetting, Value: tt.theme},
			})
			m.Update(colorSchemeMsg{light: !tt.light})
			m.Update(colorSchemeMsg{light: tt.light})
			if got := current().Name; got != tt.want {
				t.Fatalf("theme = %q, want %q", got, tt.want)
			}
		})
	}
}

// The open dialog shows the followed theme but keeps the saved choice, so
// esc never writes the OS-picked counterpart as the user's theme.
func TestFollowColorSchemeWithSettingsOpen(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	applyTheme(themes[themeIndex("catppuccin mocha")])
	m.mode = modeSettings
	m.settings.dialog.ThemeAuto = true
	m.settings.dialog.ManualTheme = "catppuccin mocha"
	m.Update(colorSchemeMsg{light: true})
	if got := current().Name; got != "catppuccin latte" {
		t.Fatalf("theme = %q, want catppuccin latte", got)
	}
	if got := themes[m.settings.dialog.ThemeIndex].Name; got != "catppuccin latte" {
		t.Fatalf("dialog shows %q, want catppuccin latte", got)
	}
	if got := m.settings.dialog.ManualTheme; got != "catppuccin mocha" {
		t.Fatalf("manual theme = %q, want catppuccin mocha", got)
	}
}
