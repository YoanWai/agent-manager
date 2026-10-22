package ui

import (
	"strings"
	"testing"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
)

func globalWindowStyle(t *testing.T) string {
	t.Helper()
	out, err := tmuxCmd(t, "show-options", "-gv", "window-style").CombinedOutput()
	if err != nil {
		t.Fatalf("show-options window-style: %v: %s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// The pane carries the theme's own backdrop, on both sides of the split, so
// an agent that follows it renders on the side the manager is drawing.
func TestAgentPaneTheme(t *testing.T) {
	t.Cleanup(func() { applyTheme(themes[0]) })
	for _, tt := range []struct {
		theme string
		fgbg  string
	}{
		{"catppuccin latte", "0;15"},
		{"tokyo night", "15;0"},
	} {
		want := themes[themeIndex(tt.theme)]
		applyTheme(want)
		got := agentPaneTheme()
		if got.Background != want.Bg {
			t.Errorf("%s: pane background = %q, want %q", tt.theme, got.Background, want.Bg)
		}
		if got.Foreground != want.Text {
			t.Errorf("%s: pane foreground = %q, want %q", tt.theme, got.Foreground, want.Text)
		}
		if got.ColorFgBg != tt.fgbg {
			t.Errorf("%s: COLORFGBG = %q, want %q", tt.theme, got.ColorFgBg, tt.fgbg)
		}
	}
}

// Agents resolve their own palette against the pane background, so a theme
// switch has to reach the tmux server as well as the manager's own frame.
func TestThemeSwitchPushesPaneBackground(t *testing.T) {
	m := buildModel(t)
	t.Cleanup(func() { applyTheme(themes[0]) })
	// Global options only stick while a server is up, and a server with no
	// sessions exits at once, so one session holds it open.
	if err := m.services.tmux.Create("panebg", "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { m.services.tmux.Kill("panebg") })
	t.Cleanup(func() { tmuxCmd(t, "set-option", "-gu", "window-style").Run() })

	m.openSettings()
	m.settings.dialog.Field = uisettings.FieldTheme

	light := themes[themeIndex("solarized light")]
	m.settings.dialog.ThemeIndex = themeIndex("solarized light") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	if m.errBar.text != "" {
		t.Fatalf("pane theme push reported %q", m.errBar.text)
	}
	if got, want := globalWindowStyle(t), "fg="+light.Text+",bg="+light.Bg; got != want {
		t.Errorf("light theme pushed window-style %q, want its own backdrop %q", got, want)
	}

	m.settings.dialog.ThemeIndex = themeIndex("nord") - 1
	if cmd := m.cycleSetting(1); cmd != nil {
		if msg := cmd(); msg != nil {
			m.Update(msg)
		}
	}
	nord := themes[themeIndex("nord")]
	if got, want := globalWindowStyle(t), "fg="+nord.Text+",bg="+nord.Bg; got != want {
		t.Errorf("dark theme pushed window-style %q, want %q", got, want)
	}
}
