package review

import (
	"fmt"
	"strings"
	"testing"

	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCommitRailOverflowLabelsKeepMouseRows(t *testing.T) {
	m := New(false)
	m.scope = git.ScopeCommit
	for i := range 15 {
		m.commits = append(m.commits, git.Commit{SHA: fmt.Sprint(i), Short: fmt.Sprint(i), Subject: "literal ← subject"})
	}
	m.commitPin.SHA = m.commits[7].SHA
	for _, tc := range []struct {
		mode         presentation.GlyphMode
		above, below string
	}{
		{presentation.GlyphModeBasic, "more above", "more below"},
		{presentation.GlyphModeAdvanced, "↑ ", "↓ "},
	} {
		view := ansi.Strip(m.CommitRail(RenderContext{GlyphMode: tc.mode}, 60, 5))
		rows := strings.Split(view, "\n")
		if !strings.Contains(rows[0], tc.above) || !strings.Contains(rows[len(rows)-1], tc.below) || !strings.Contains(view, "literal ← subject") {
			t.Fatalf("%s: %s", tc.mode, view)
		}
		for _, line := range []int{0, len(rows) - 1} {
			sha, ok := m.ClickCommit(line)
			if !ok || sha == "" {
				t.Fatalf("%s row %d lost mouse target", tc.mode, line)
			}
		}
	}
}
