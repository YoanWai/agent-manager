package theme

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/systheme"
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
			if got := Auto(tt.stored, tt.scheme); got != tt.want {
				t.Errorf("Auto(%q, %v) = %q, want %q", tt.stored, tt.scheme, got, tt.want)
			}
		})
	}
}
