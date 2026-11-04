package frame

import (
	"strings"
	"testing"
)

func TestHeaderRowDropsVersionFirst(t *testing.T) {
	h := Header{
		Left:    "agent-manager",
		Version: " v0.40.1",
		Tag:     "  ↑ v0.41.0 available",
		Scope:   "repo",
		Rollup:  []string{"3 working", ""},
		Sep:     "   ",
		Gutter:  2,
	}
	for _, tc := range []struct {
		width int
		want  string
	}{
		{80, "agent-manager v0.40.1  ↑ v0.41.0 available" + strings.Repeat(" ", 20) + "repo   3 working  "},
		{60, "agent-manager  ↑ v0.41.0 available" + strings.Repeat(" ", 8) + "repo   3 working  "},
		{44, "agent-manager  ↑ v0.41.0 available" + strings.Repeat(" ", 4) + "repo  "},
	} {
		h.Width = tc.width
		if got := h.Row(); got != tc.want {
			t.Errorf("width %d:\n got %q\nwant %q", tc.width, got, tc.want)
		}
	}
}
