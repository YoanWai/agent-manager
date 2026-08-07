package sysstat

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ColorScheme represents the system's color mode preference
type ColorScheme string

const (
	ColorSchemeLight ColorScheme = "light"
	ColorSchemeDark  ColorScheme = "dark"
	ColorSchemeUnknown ColorScheme = "unknown"
)

// DetectSystemColorScheme returns the system's preferred color scheme (light or dark).
// Returns ColorSchemeUnknown if detection fails or is not supported on the platform.
func DetectSystemColorScheme() ColorScheme {
	switch runtime.GOOS {
	case "darwin":
		return detectDarwinColorScheme()
	case "windows":
		return detectWindowsColorScheme()
	case "linux":
		return detectLinuxColorScheme()
	default:
		return ColorSchemeUnknown
	}
}

// detectDarwinColorScheme detects macOS system appearance
func detectDarwinColorScheme() ColorScheme {
	// Try AppleInterfaceStyle first (most reliable)
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	if err == nil {
		style := strings.TrimSpace(string(out))
		if style == "Dark" {
			return ColorSchemeDark
		}
		return ColorSchemeLight
	}

	// Fallback: check NSRequiresAquaSystemAppearance
	// If this is set to true, the app should use light mode
	// If false or not set, it can use dark mode
	out, err = exec.Command("defaults", "read", "-g", "NSRequiresAquaSystemAppearance").Output()
	if err == nil {
		style := strings.TrimSpace(string(out))
		if style == "1" || strings.ToLower(style) == "true" {
			return ColorSchemeLight
		}
		return ColorSchemeDark
	}

	return ColorSchemeUnknown
}

// detectWindowsColorScheme detects Windows system appearance
func detectWindowsColorScheme() ColorScheme {
	// Check the registry for AppsUseLightTheme
	// HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize
	out, err := exec.Command("reg", "query", 
		`HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, 
		"/v", "AppsUseLightTheme").Output()
	if err == nil {
		output := string(out)
		if strings.Contains(output, "0x1") || strings.Contains(output, "1") {
			return ColorSchemeLight
		}
		return ColorSchemeDark
	}
	return ColorSchemeUnknown
}

// detectLinuxColorScheme detects Linux system appearance
// Uses various environment variables and tools commonly available on Linux
func detectLinuxColorScheme() ColorScheme {
	// Try XDG desktop portal first (most modern)
	if scheme := detectXDGPortal(); scheme != ColorSchemeUnknown {
		return scheme
	}

	// Try gsettings (GNOME)
	if scheme := detectGSettings(); scheme != ColorSchemeUnknown {
		return scheme
	}

	// Try environment variables
	if scheme := detectEnvVars(); scheme != ColorSchemeUnknown {
		return scheme
	}

	return ColorSchemeUnknown
}

// detectXDGPortal uses xdg-desktop-portal to get color scheme
func detectXDGPortal() ColorScheme {
	out, err := exec.Command("dbus-send", 
		"--print-reply=literal", 
		"--dest=org.freedesktop.portal.Desktop",
		"/org/freedesktop/portal/desktop",
		"org.freedesktop.DBus.Properties.Get",
		"string:org.freedesktop.portal.Settings",
		"string:color-scheme").Output()
	if err == nil {
		output := string(out)
		if strings.Contains(output, "uint32 1") || strings.Contains(output, "1") {
			return ColorSchemeDark
		}
		if strings.Contains(output, "uint32 0") || strings.Contains(output, "0") {
			return ColorSchemeLight
		}
	}
	return ColorSchemeUnknown
}

// detectGSettings uses gsettings to get the GTK theme preference
func detectGSettings() ColorScheme {
	// Try the gsettings approach for GTK-based desktops
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "color-scheme").Output()
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
			return detectGTKTheme()
		}
	}

	// Fallback: try the gtk-theme-name setting
	return detectGTKTheme()
}

// detectGTKTheme checks the GTK theme name for dark variants
func detectGTKTheme() ColorScheme {
	out, err := exec.Command("gsettings", "get", "org.gnome.desktop.interface", "gtk-theme").Output()
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

// detectEnvVars checks common environment variables for color scheme
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

// parseHexColor parses a 2-character hex string into a 0-255 integer
func parseHexColor(hexStr string) int {
	var val int
	if _, err := fmt.Sscanf(hexStr, "%02x", &val); err == nil {
		return val
	}
	return 0
}

// ThemeForColorScheme returns the recommended agent-manager theme name
// for the given color scheme. Returns the dark "classic" theme for dark mode
// and the light "solarized light" theme for light mode.
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
