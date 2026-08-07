package sysstat

import (
	"errors"
	"os/exec"
	"testing"
)

func TestDetectSystemColorScheme(t *testing.T) {
	// Test that the function returns a valid ColorScheme for all platforms
	// We can't easily mock the system calls, so we just verify the return type
	scheme := DetectSystemColorScheme()
	if scheme != ColorSchemeLight && scheme != ColorSchemeDark && scheme != ColorSchemeUnknown {
		t.Errorf("DetectSystemColorScheme() returned unexpected value: %s", scheme)
	}
}

func TestThemeForColorScheme(t *testing.T) {
	tests := []struct {
		name     string
		scheme   ColorScheme
		expected string
	}{
		{"dark scheme", ColorSchemeDark, "classic"},
		{"light scheme", ColorSchemeLight, "solarized light"},
		{"unknown scheme", ColorSchemeUnknown, "classic"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ThemeForColorScheme(tt.scheme)
			if result != tt.expected {
				t.Errorf("ThemeForColorScheme(%s) = %s, want %s", tt.scheme, result, tt.expected)
			}
		})
	}
}

// MockCommandRunner is a mock implementation of CommandRunner for testing.
type MockCommandRunner struct {
	// commands maps command name + args to (output, error)
	commands map[string][]byte
	errors   map[string]error
}

// Run implements CommandRunner interface for mock.
func (m *MockCommandRunner) Run(name string, args ...string) ([]byte, error) {
	key := name
	for _, arg := range args {
		key += " " + arg
	}
	if output, ok := m.commands[key]; ok {
		return output, nil
	}
	if err, ok := m.errors[key]; ok {
		return nil, err
	}
	// Default: command not found behavior
	return nil, &exec.ExitError{}
}

// newMockRunner creates a mock runner with the given command outputs and errors.
func newMockRunner() *MockCommandRunner {
	return &MockCommandRunner{
		commands: make(map[string][]byte),
		errors:   make(map[string]error),
	}
}

func TestDetectDarwinColorSchemeWithRunner(t *testing.T) {
	tests := []struct {
		name     string
		runner   *MockCommandRunner
		expected ColorScheme
	}{
		{
			name: "AppleInterfaceStyle Dark",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["defaults read -g AppleInterfaceStyle"] = []byte("Dark\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "AppleInterfaceStyle Light",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["defaults read -g AppleInterfaceStyle"] = []byte("Light\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "AppleInterfaceStyle missing key, falls back to NSRequiresAquaSystemAppearance true",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				// Simulate key not found error (exit code 1)
				r.errors["defaults read -g AppleInterfaceStyle"] = &exec.ExitError{}
				r.commands["defaults read -g NSRequiresAquaSystemAppearance"] = []byte("1\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "AppleInterfaceStyle missing key, NSRequiresAquaSystemAppearance false",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["defaults read -g AppleInterfaceStyle"] = &exec.ExitError{}
				r.commands["defaults read -g NSRequiresAquaSystemAppearance"] = []byte("0\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "AppleInterfaceStyle missing key, NSRequiresAquaSystemAppearance missing, defaults to light",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["defaults read -g AppleInterfaceStyle"] = &exec.ExitError{}
				r.errors["defaults read -g NSRequiresAquaSystemAppearance"] = &exec.ExitError{}
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "defaults command fails",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["defaults read -g AppleInterfaceStyle"] = errors.New("command not found")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectDarwinColorSchemeWithRunner(tt.runner)
			if result != tt.expected {
				t.Errorf("detectDarwinColorSchemeWithRunner() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectWindowsColorSchemeWithRunner(t *testing.T) {
	tests := []struct {
		name     string
		runner   *MockCommandRunner
		expected ColorScheme
	}{
		{
			name: "AppsUseLightTheme = 1 (light mode)",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["reg query HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize /v AppsUseLightTheme"] = []byte("AppsUseLightTheme    REG_DWORD    0x1\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "AppsUseLightTheme = 0 (dark mode)",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["reg query HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize /v AppsUseLightTheme"] = []byte("AppsUseLightTheme    REG_DWORD    0x0\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "reg command fails",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["reg query HKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize /v AppsUseLightTheme"] = errors.New("registry not found")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectWindowsColorSchemeWithRunner(tt.runner)
			if result != tt.expected {
				t.Errorf("detectWindowsColorSchemeWithRunner() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectXDGPortalWithRunner(t *testing.T) {
	tests := []struct {
		name     string
		runner   *MockCommandRunner
		expected ColorScheme
	}{
		{
			name: "color-scheme = 1 (dark)",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands[`dbus-send --print-reply=literal --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop org.freedesktop.DBus.Properties.Get string:org.freedesktop.portal.Settings string:color-scheme`] = []byte("uint32 1\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "color-scheme = 2 (light)",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands[`dbus-send --print-reply=literal --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop org.freedesktop.DBus.Properties.Get string:org.freedesktop.portal.Settings string:color-scheme`] = []byte("uint32 2\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "color-scheme = 0 (unknown)",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands[`dbus-send --print-reply=literal --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop org.freedesktop.DBus.Properties.Get string:org.freedesktop.portal.Settings string:color-scheme`] = []byte("uint32 0\n")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
		{
			name: "dbus-send command fails",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors[`dbus-send --print-reply=literal --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop org.freedesktop.DBus.Properties.Get string:org.freedesktop.portal.Settings string:color-scheme`] = errors.New("dbus not available")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectXDGPortalWithRunner(tt.runner)
			if result != tt.expected {
				t.Errorf("detectXDGPortalWithRunner() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectGSettingsWithRunner(t *testing.T) {
	tests := []struct {
		name     string
		runner   *MockCommandRunner
		expected ColorScheme
	}{
		{
			name: "color-scheme prefer-dark",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface color-scheme"] = []byte("'prefer-dark'\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "color-scheme prefer-light",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface color-scheme"] = []byte("'prefer-light'\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "color-scheme default, falls back to GTK theme dark",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface color-scheme"] = []byte("'default'\n")
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'Adwaita-dark'\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "color-scheme default, falls back to GTK theme light",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface color-scheme"] = []byte("'default'\n")
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'Adwaita'\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "gsettings command fails, falls back to GTK theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["gsettings get org.gnome.desktop.interface color-scheme"] = errors.New("gsettings not found")
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'Adwaita-dark'\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "both gsettings commands fail",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["gsettings get org.gnome.desktop.interface color-scheme"] = errors.New("gsettings not found")
				r.errors["gsettings get org.gnome.desktop.interface gtk-theme"] = errors.New("gsettings not found")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectGSettingsWithRunner(tt.runner)
			if result != tt.expected {
				t.Errorf("detectGSettingsWithRunner() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectGTKThemeWithRunner(t *testing.T) {
	tests := []struct {
		name     string
		runner   *MockCommandRunner
		expected ColorScheme
	}{
		{
			name: "Adwaita-dark theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'Adwaita-dark'\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "Adwaita light theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'Adwaita'\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "dracula theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'dracula'\n")
				return r
			}(),
			expected: ColorSchemeDark,
		},
		{
			name: "solarized-light theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'solarized-light'\n")
				return r
			}(),
			expected: ColorSchemeLight,
		},
		{
			name: "unknown theme",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.commands["gsettings get org.gnome.desktop.interface gtk-theme"] = []byte("'unknown-theme'\n")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
		{
			name: "gsettings command fails",
			runner: func() *MockCommandRunner {
				r := newMockRunner()
				r.errors["gsettings get org.gnome.desktop.interface gtk-theme"] = errors.New("gsettings not found")
				return r
			}(),
			expected: ColorSchemeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectGTKThemeWithRunner(tt.runner)
			if result != tt.expected {
				t.Errorf("detectGTKThemeWithRunner() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestParseHexColor(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"00", 0},
		{"ff", 255},
		{"a0", 160},
		{"0a", 10},
		{"FF", 255},
		{"", 0},
		{"gg", 0}, // invalid hex
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseHexColor(tt.input)
			if result != tt.expected {
				t.Errorf("parseHexColor(%s) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectEnvVars(t *testing.T) {
	// Save and restore environment
	t.Setenv("DARK_MODE", "")
	t.Setenv("COLORFGBG", "")

	// Test with DARK_MODE=true
	t.Setenv("DARK_MODE", "true")
	t.Setenv("COLORFGBG", "")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with DARK_MODE=true = %s, want %s", result, ColorSchemeDark)
	}

	// Test with DARK_MODE=1
	t.Setenv("DARK_MODE", "1")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with DARK_MODE=1 = %s, want %s", result, ColorSchemeDark)
	}

	// Test with DARK_MODE=false
	t.Setenv("DARK_MODE", "false")
	if result := detectEnvVars(); result != ColorSchemeLight {
		t.Errorf("detectEnvVars() with DARK_MODE=false = %s, want %s", result, ColorSchemeLight)
	}

	// Test with COLORFGBG indicating dark background
	t.Setenv("DARK_MODE", "")
	t.Setenv("COLORFGBG", "ffffff;000000")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with dark COLORFGBG = %s, want %s", result, ColorSchemeDark)
	}

	// Test with COLORFGBG indicating light background
	t.Setenv("COLORFGBG", "000000;ffffff")
	if result := detectEnvVars(); result != ColorSchemeLight {
		t.Errorf("detectEnvVars() with light COLORFGBG = %s, want %s", result, ColorSchemeLight)
	}

	// Test with no environment variables set
	t.Setenv("DARK_MODE", "")
	t.Setenv("COLORFGBG", "")
	if result := detectEnvVars(); result != ColorSchemeUnknown {
		t.Errorf("detectEnvVars() with no env vars = %s, want %s", result, ColorSchemeUnknown)
	}
}

// Test that the ColorScheme type has the expected string values
func TestColorSchemeStringValues(t *testing.T) {
	if string(ColorSchemeLight) != "light" {
		t.Errorf("ColorSchemeLight string value = %s, want light", string(ColorSchemeLight))
	}
	if string(ColorSchemeDark) != "dark" {
		t.Errorf("ColorSchemeDark string value = %s, want dark", string(ColorSchemeDark))
	}
	if string(ColorSchemeUnknown) != "unknown" {
		t.Errorf("ColorSchemeUnknown string value = %s, want unknown", string(ColorSchemeUnknown))
	}
}

// Mock for backward compatibility - these tests can be removed once all tests use runners
func TestDetectDarwinColorScheme(t *testing.T) {
	t.Skip("Use TestDetectDarwinColorSchemeWithRunner instead")
}

func TestDetectWindowsColorSchemeNoPanic(t *testing.T) {
	t.Skip("Use TestDetectWindowsColorSchemeWithRunner instead")
}

func TestDetectLinuxColorSchemeNoPanic(t *testing.T) {
	t.Skip("Use detectLinuxColorSchemeWithRunner tests instead")
}

// Test that the defaults reads are non-panicking
func TestDetectDarwinColorSchemeNoPanic(t *testing.T) {
	// This test just ensures the function doesn't panic on non-macOS systems
	runner := newMockRunner()
	runner.errors["defaults read -g AppleInterfaceStyle"] = errors.New("not on macOS")
	result := detectDarwinColorSchemeWithRunner(runner)
	// Should return unknown when defaults fails completely
	if result != ColorSchemeUnknown {
		t.Errorf("detectDarwinColorSchemeWithRunner() = %s, want %s", result, ColorSchemeUnknown)
	}
}