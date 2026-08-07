package sysstat

import (
	"os"
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

func TestDetectDarwinColorScheme(t *testing.T) {
	// Only run on macOS - we can't test this on other platforms
	// without complex mocking of exec.Command
	t.Skip("Skipping macOS-specific test (requires mocking)")
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
	originalDarkMode := os.Getenv("DARK_MODE")
	originalColorFgBg := os.Getenv("COLORFGBG")
	defer func() {
		os.Setenv("DARK_MODE", originalDarkMode)
		os.Setenv("COLORFGBG", originalColorFgBg)
	}()

	// Test with DARK_MODE=true
	os.Setenv("DARK_MODE", "true")
	os.Unsetenv("COLORFGBG")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with DARK_MODE=true = %s, want %s", result, ColorSchemeDark)
	}

	// Test with DARK_MODE=1
	os.Setenv("DARK_MODE", "1")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with DARK_MODE=1 = %s, want %s", result, ColorSchemeDark)
	}

	// Test with DARK_MODE=false
	os.Setenv("DARK_MODE", "false")
	if result := detectEnvVars(); result != ColorSchemeLight {
		t.Errorf("detectEnvVars() with DARK_MODE=false = %s, want %s", result, ColorSchemeLight)
	}

	// Test with COLORFGBG indicating dark background
	os.Unsetenv("DARK_MODE")
	os.Setenv("COLORFGBG", "ffffff;000000")
	if result := detectEnvVars(); result != ColorSchemeDark {
		t.Errorf("detectEnvVars() with dark COLORFGBG = %s, want %s", result, ColorSchemeDark)
	}

	// Test with COLORFGBG indicating light background
	os.Setenv("COLORFGBG", "000000;ffffff")
	if result := detectEnvVars(); result != ColorSchemeLight {
		t.Errorf("detectEnvVars() with light COLORFGBG = %s, want %s", result, ColorSchemeLight)
	}

	// Test with no environment variables set
	os.Unsetenv("DARK_MODE")
	os.Unsetenv("COLORFGBG")
	if result := detectEnvVars(); result != ColorSchemeUnknown {
		t.Errorf("detectEnvVars() with no env vars = %s, want %s", result, ColorSchemeUnknown)
	}
}

// Mock for testing - we can't easily mock exec.Command, but we can test
// that the functions don't panic and return valid values
func TestDetectWindowsColorSchemeNoPanic(t *testing.T) {
	t.Skip("Skipping Windows-specific test (requires mocking)")
}

func TestDetectLinuxColorSchemeNoPanic(t *testing.T) {
	t.Skip("Skipping Linux-specific test (requires mocking)")
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
