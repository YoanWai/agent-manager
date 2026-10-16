//go:build windows

package tmux

import (
	"strings"
	"testing"
)

func TestParsePsmuxVersion(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		wantErr string
	}{
		{"current release", "tmux 3.4.0\npsmux 3.4.0 (20ef00fb 2026-10-14)\n", ""},
		{"newer", "psmux 3.5.1", ""},
		{"too old", "psmux 3.3.8", "psmux 3.4.0"},
		{"no psmux line, the 3.3.x shape", "tmux 3.3.6", "unknown"},
		{"unparseable", "psmux banana", "banana"},
	}
	for _, tc := range cases {
		err := parsePsmuxVersion(tc.out)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("%s: parsePsmuxVersion(%q) = %v, want nil", tc.name, tc.out, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: parsePsmuxVersion(%q) = nil, want an error", tc.name, tc.out)
			continue
		}
		if !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %q does not mention %q", tc.name, err, tc.wantErr)
		}
	}
}
