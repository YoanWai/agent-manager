package systheme

import (
	"errors"
	"os/exec"
	"testing"
)

func fakeRun(outputs map[string]string, errs map[string]error) runner {
	return func(name string, args ...string) ([]byte, error) {
		key := name
		for _, arg := range args {
			key += " " + arg
		}
		if err, ok := errs[key]; ok {
			return nil, err
		}
		if out, ok := outputs[key]; ok {
			return []byte(out), nil
		}
		return nil, errors.New("unexpected command: " + key)
	}
}

const darwinKey = "defaults read -g AppleInterfaceStyle"

func TestDarwinScheme(t *testing.T) {
	tests := []struct {
		name string
		run  runner
		want Scheme
	}{
		{"dark", fakeRun(map[string]string{darwinKey: "Dark\n"}, nil), SchemeDark},
		{"missing key means light", fakeRun(nil, map[string]error{darwinKey: &exec.ExitError{}}), SchemeLight},
		{"unexpected value reads light", fakeRun(map[string]string{darwinKey: "Auto\n"}, nil), SchemeLight},
		{"defaults unavailable", fakeRun(nil, map[string]error{darwinKey: errors.New("not found")}), SchemeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := darwinScheme(tt.run); got != tt.want {
				t.Errorf("darwinScheme() = %v, want %v", got, tt.want)
			}
		})
	}
}

const (
	portalKey   = "dbus-send --session --type=method_call --print-reply=literal --dest=org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop org.freedesktop.portal.Settings.Read string:org.freedesktop.appearance string:color-scheme"
	schemeKey   = "gsettings get org.gnome.desktop.interface color-scheme"
	gtkThemeKey = "gsettings get org.gnome.desktop.interface gtk-theme"
)

func TestLinuxScheme(t *testing.T) {
	noPortal := map[string]error{portalKey: errors.New("no bus")}
	tests := []struct {
		name    string
		outputs map[string]string
		errs    map[string]error
		want    Scheme
	}{
		{"portal dark", map[string]string{portalKey: "   variant       uint32 1\n"}, nil, SchemeDark},
		{"portal light", map[string]string{portalKey: "   variant       uint32 2\n"}, nil, SchemeLight},
		{"portal no preference falls to gsettings",
			map[string]string{portalKey: "   variant       uint32 0\n", schemeKey: "'prefer-dark'\n", gtkThemeKey: "'Adwaita'\n"}, nil, SchemeDark},
		{"gsettings prefer-light", map[string]string{schemeKey: "'prefer-light'\n"}, noPortal, SchemeLight},
		{"gsettings default with dark gtk theme",
			map[string]string{schemeKey: "'default'\n", gtkThemeKey: "'Adwaita-dark'\n"}, noPortal, SchemeDark},
		{"gsettings default with light gtk theme",
			map[string]string{schemeKey: "'default'\n", gtkThemeKey: "'Adwaita'\n"}, noPortal, SchemeUnknown},
		{"nothing available", nil,
			map[string]error{portalKey: errors.New("x"), schemeKey: errors.New("x"), gtkThemeKey: errors.New("x")}, SchemeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linuxScheme(fakeRun(tt.outputs, tt.errs)); got != tt.want {
				t.Errorf("linuxScheme() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTerminalScheme(t *testing.T) {
	answered := func(r, g, b int) func() (int, int, int, bool) {
		return func() (int, int, int, bool) { return r, g, b, true }
	}
	silent := func() (int, int, int, bool) { return 0, 0, 0, false }
	env := func(value string) func(string) string {
		return func(string) string { return value }
	}
	tests := []struct {
		name  string
		query func() (int, int, int, bool)
		env   func(string) string
		want  Scheme
	}{
		{"dark background", answered(15, 17, 21), env(""), SchemeDark},
		{"light background", answered(253, 246, 227), env(""), SchemeLight},
		{"silent terminal, dark COLORFGBG", silent, env("15;0"), SchemeDark},
		{"silent terminal, light COLORFGBG", silent, env("0;15"), SchemeLight},
		{"silent terminal, default COLORFGBG", silent, env("default;default"), SchemeUnknown},
		{"silent terminal, nothing set", silent, env(""), SchemeUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := terminalScheme(tt.query, tt.env); got != tt.want {
				t.Errorf("terminalScheme() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseOSC11(t *testing.T) {
	tests := []struct {
		name     string
		response string
		r, g, b  int
		ok       bool
	}{
		{"xterm 16-bit, ST", "\x1b]11;rgb:0f0f/1111/1515\x1b\\", 15, 17, 21, true},
		{"8-bit, BEL", "\x1b]11;rgb:fd/f6/e3\a", 253, 246, 227, true},
		{"4-bit channels", "\x1b]11;rgb:f/f/f\a", 255, 255, 255, true},
		{"no color spec", "\x1b]11;?\a", 0, 0, 0, false},
		{"wrong channel count", "\x1b]11;rgb:aa/bb\a", 0, 0, 0, false},
		{"garbage", "hello", 0, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, g, b, ok := parseOSC11(tt.response)
			if r != tt.r || g != tt.g || b != tt.b || ok != tt.ok {
				t.Errorf("parseOSC11(%q) = %d,%d,%d,%v want %d,%d,%d,%v",
					tt.response, r, g, b, ok, tt.r, tt.g, tt.b, tt.ok)
			}
		})
	}
}

func TestColorFgBgScheme(t *testing.T) {
	tests := []struct {
		value string
		want  Scheme
	}{
		{"15;0", SchemeDark},
		{"0;15", SchemeLight},
		{"12;8", SchemeDark},
		{"0;7", SchemeLight},
		{"15;default;0", SchemeDark},
		{"default;default", SchemeUnknown},
		{"", SchemeUnknown},
		{"15", SchemeUnknown},
		{"0;12", SchemeUnknown},
	}
	for _, tt := range tests {
		if got := colorFgBgScheme(tt.value); got != tt.want {
			t.Errorf("colorFgBgScheme(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}
