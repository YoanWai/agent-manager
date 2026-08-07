package sysstat

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type ColorScheme string

const (
	ColorSchemeLight   ColorScheme = "light"
	ColorSchemeDark    ColorScheme = "dark"
	ColorSchemeUnknown ColorScheme = "unknown"
)

// CommandRunner is an interface for running system commands, used for testing.
type CommandRunner interface {
	Run(name string, args ...string) ([]byte, error)
}

// RealCommandRunner uses the actual exec.Command to run system commands.
type RealCommandRunner struct{}

// Run executes the given command and returns its output.
func (RealCommandRunner) Run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

// DefaultRunner is the default command runner that uses exec.Command.
var DefaultRunner CommandRunner = RealCommandRunner{}

// DetectSystemColorScheme returns the system's preferred color scheme (light or dark).
// Returns ColorSchemeUnknown if detection fails or is not supported on the platform.
func DetectSystemColorScheme() ColorScheme {
	return DetectSystemColorSchemeWithRunner(DefaultRunner)
}

// DetectSystemColorSchemeWithRunner allows passing a custom command runner for testing.
func DetectSystemColorSchemeWithRunner(runner CommandRunner) ColorScheme {
	switch runtime.GOOS {
	case "darwin":
		return detectDarwinColorSchemeWithRunner(runner)
	case "windows":
		return detectWindowsColorSchemeWithRunner(runner)
	case "linux":
		return detectLinuxColorSchemeWithRunner(runner)
	default:
		return ColorSchemeUnknown
	}
}

func detectDarwinColorScheme() ColorScheme {
	return detectDarwinColorSchemeWithRunner(DefaultRunner)
}

func detectDarwinColorSchemeWithRunner(runner CommandRunner) ColorScheme {
	// Try AppleInterfaceStyle first (most reliable)
	out, err := runner.Run("defaults", "read", "-g", "AppleInterfaceStyle")
	if err == nil {
		style := strings.TrimSpace(string(out))
		if style == "Dark" {
			return ColorSchemeDark
		}
		return ColorSchemeLight
	}

	// When defaults launches successfully but the key is missing,
	// macOS defaults to light mode
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return ColorSchemeUnknown
	}

	// Fallback: check NSRequiresAquaSystemAppearance
	// If this is set to true, the app should use light mode
	out, err = runner.Run("defaults", "read", "-g", "NSRequiresAquaSystemAppearance")
	if err == nil {
		style := strings.TrimSpace(string(out))
		if style == "1" || strings.ToLower(style) == "true" {
			return ColorSchemeLight
		}
		return ColorSchemeDark
	}

	// Same logic for NSRequiresAquaSystemAppearance
	if err != nil && !errors.As(err, &exitErr) {
		return ColorSchemeUnknown
	}

	return ColorSchemeLight
}

func detectWindowsColorScheme() ColorScheme {
	return detectWindowsColorSchemeWithRunner(DefaultRunner)
}

func detectWindowsColorSchemeWithRunner(runner CommandRunner) ColorScheme {
	// Check the registry for AppsUseLightTheme
	// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize
	out, err := runner.Run("reg", "query",
		`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		"/v", "AppsUseLightTheme")
	if err == nil {
		output := string(out)
		if strings.Contains(output, "0x1") || strings.Contains(output, "1") {
			return ColorSchemeLight
		}
		return ColorSchemeDark
	}
	return ColorSchemeUnknown
}

func detectLinuxColorScheme() ColorScheme {
	return detectLinuxColorSchemeWithRunner(DefaultRunner)
}

func detectLinuxColorSchemeWithRunner(runner CommandRunner) ColorScheme {
	// Try XDG desktop portal first (most modern)
	if scheme := detectXDGPortalWithRunner(runner); scheme != ColorSchemeUnknown {
		return scheme
	}

	// Try gsettings (GNOME)
	if scheme := detectGSettingsWithRunner(runner); scheme != ColorSchemeUnknown {
		return scheme
	}

	// Try environment variables
	if scheme := detectEnvVars(); scheme != ColorSchemeUnknown {
		return scheme
	}

	return ColorSchemeUnknown
}

func detectXDGPortal() ColorScheme {
	return detectXDGPortalWithRunner(DefaultRunner)
}

func detectXDGPortalWithRunner(runner CommandRunner) ColorScheme {
	out, err := runner.Run("dbus-send",
		"--print-reply=literal",
		"--dest=org.freedesktop.portal.Desktop",
		"/org/freedesktop/portal/desktop",
		"org.freedesktop.DBus.Properties.Get",
		"string:org.freedesktop.portal.Settings",
		"string:color-scheme")
	if err == nil {
		output := string(out)
		// Parse the uint32 value from the dbus-send output
		// Expected format contains "uint32 <value>" where value is 0, 1, or 2
		if strings.Contains(output, "uint32 1") {
			return ColorSchemeDark
		}
		if strings.Contains(output, "uint32 2") {
			return ColorSchemeLight
		}
		// 0 or any other value means unknown/unset
		if strings.Contains(output, "uint32 0") {
			return ColorSchemeUnknown
		}
	}
	return ColorSchemeUnknown
}

func detectGSettings() ColorScheme {
	return detectGSettingsWithRunner(DefaultRunner)
}

func detectGSettingsWithRunner(runner CommandRunner) ColorScheme {
	// Try the gsettings approach for GTK-based desktops
	out, err := runner.Run("gsettings", "get", "org.gnome.desktop.interface", "color-scheme")
	if err == nil {
		output := strings.TrimSpace(string(out))
		if output == "'prefer-dark'" || output == "prefer-dark" {
			return ColorSchemeDark
		}
		if output == "'prefer-light'" || output == "prefer-light" {
			return ColorSchemeLight
		}
		if output == "'default'" || output == "default" {
			// Try to detect from GTK theme name
			return detectGTKThemeWithRunner(runner)
		}
	}

	// Fallback: try the gtk-theme-name setting
	return detectGTKThemeWithRunner(runner)
}

func detectGTKTheme() ColorScheme {
	return detectGTKThemeWithRunner(DefaultRunner)
}

func detectGTKThemeWithRunner(runner CommandRunner) ColorScheme {
	out, err := runner.Run("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme")
	if err == nil {
		theme := strings.TrimSpace(string(out))
		theme = strings.Trim(theme, "'\"")
		// Common dark theme identifiers
		darkThemes := []string{"Adwaita-dark", "dark", "night", "black", "dracula", "nord", "solarized-dark"}
		for _, dark := range darkThemes {
			if strings.Contains(strings.ToLower(theme), strings.ToLower(dark)) {
				return ColorSchemeDark
			}
		}
		// Common light theme identifiers
		lightThemes := []string{"Adwaita", "light", "white", "paper", "solarized-light"}
		for _, light := range lightThemes {
			if strings.Contains(strings.ToLower(theme), strings.ToLower(light)) {
				return ColorSchemeLight
			}
		}
	}
	return ColorSchemeUnknown
}

func detectEnvVars() ColorScheme {
	// Check common environment variables that indicate color scheme
	// DARK_MODE is used by some applications
	if darkMode := os.Getenv("DARK_MODE"); darkMode != "" {
		if darkMode == "1" || strings.ToLower(darkMode) == "true" {
			return ColorSchemeDark
		}
		return ColorSchemeLight
	}

	// COLORFGBG is used by some terminals (format: "fg;bg" in hex RRGGBB)
	// Dark backgrounds typically have low RGB values
	if colorFgBg := os.Getenv("COLORFGBG"); colorFgBg != "" {
		parts := strings.Split(colorFgBg, ";")
		if len(parts) >= 2 {
			// Parse the background color (second part)
			bgHex := parts[1]
			if len(bgHex) >= 6 {
				// Extract RGB components
				r, g, b := parseHexColor(bgHex[:2]), parseHexColor(bgHex[2:4]), parseHexColor(bgHex[4:6])
				// Calculate relative luminance
				luminance := 0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)
				// If luminance is low, it's likely a dark theme
				if luminance < 130 {
					return ColorSchemeDark
				}
				return ColorSchemeLight
			}
		}
	}

	return ColorSchemeUnknown
}

func parseHexColor(hexStr string) int {
	var val int
	if _, err := fmt.Sscanf(hexStr, "%02x", &val); err == nil {
		return val
	}
	return 0
}

func ThemeForColorScheme(scheme ColorScheme) string {
	switch scheme {
	case ColorSchemeDark:
		return "classic" // or any other dark theme
	case ColorSchemeLight:
		return "solarized light" // or any other light theme
	default:
		return "classic" // fallback to default
	}
}