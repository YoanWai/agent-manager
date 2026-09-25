package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureGrokTerminalThemeFile(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "missing file",
			want: "[ui]\ntheme = \"terminal\"\n\n[features]\nterminal_theme = true\n",
		},
		{
			name: "adds both sections",
			in:   "screen_mode = \"fullscreen\"\n",
			want: "screen_mode = \"fullscreen\"\n\n[ui]\ntheme = \"terminal\"\n\n[features]\nterminal_theme = true\n",
		},
		{
			name: "replaces an existing theme and adds the flag",
			in:   "[ui]\ntheme = \"groknight\"\nscreen_mode = \"fullscreen\"\n",
			want: "[ui]\ntheme = \"terminal\"\nscreen_mode = \"fullscreen\"\n\n[features]\nterminal_theme = true\n",
		},
		{
			name: "fills the keys inside existing sections",
			in:   "[ui]\nscreen_mode = \"fullscreen\"\n\n[ui.display_refresh]\nauto_cadence_enabled = true\n\n[features]\ntelemetry = false\n",
			want: "[ui]\nscreen_mode = \"fullscreen\"\ntheme = \"terminal\"\n\n[ui.display_refresh]\nauto_cadence_enabled = true\n\n[features]\ntelemetry = false\nterminal_theme = true\n",
		},
		{
			name: "leaves a finished file alone",
			in:   "[ui]\ntheme = \"terminal\"\n\n[features]\nterminal_theme = true\n",
			want: "[ui]\ntheme = \"terminal\"\n\n[features]\nterminal_theme = true\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".grok", "config.toml")
			if tc.in != "" {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(tc.in), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if err := ensureGrokTerminalThemeFile(path); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("config =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}
