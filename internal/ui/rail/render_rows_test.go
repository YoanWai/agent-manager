package rail

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func testRenderer() renderer {
	return renderer{ctx: RenderContext{GlyphMode: presentation.GlyphModeAdvanced, Theme: theme.Palettes[0]}, palette: newPalette(theme.Palettes[0])}
}

func TestCompactCellIsStatePicked(t *testing.T) {
	r := testRenderer()
	idle := Session{ID: "notes", Status: "idle", Prompt: "verify the staging deploy is healthy", PaneLine: "All quiet, nothing queued."}
	line := ansi.Strip(r.compactCell(idle, r.palette.subtle, 80))
	if line != "↳ All quiet, nothing queued." {
		t.Fatalf("an idle session that has spoken should quote its reply, not its task: %q", line)
	}
	idle.PaneLine = ""
	if line := ansi.Strip(r.compactCell(idle, r.palette.subtle, 80)); line != "❯ verify the staging deploy is healthy" {
		t.Fatalf("a silent idle session should name its task: %q", line)
	}
	waiting := Session{ID: "db", Status: "waiting", PaneLine: "Allow edits to router.go?"}
	if line := ansi.Strip(r.compactCell(waiting, r.palette.subtle, 80)); line != "↳ Allow edits to router.go?" {
		t.Fatalf("a waiting row should quote its question: %q", line)
	}
}

func TestTallRowQuotesTheReplyAndDashesWhenSilent(t *testing.T) {
	r := testRenderer()
	sess := Session{ID: "notes", Status: "idle", PaneLine: "All quiet, nothing queued."}
	lines := strings.Split(ansi.Strip(r.tallRow(sess, "head", "", "  ", false, 80, "")), "\n")
	if reply := strings.TrimSpace(lines[2]); reply != "↳ All quiet, nothing queued." {
		t.Fatalf("idle reply line = %q", reply)
	}
	sess.PaneLine = ""
	lines = strings.Split(ansi.Strip(r.tallRow(sess, "head", "", "  ", false, 80, "")), "\n")
	if reply := strings.TrimSpace(lines[2]); reply != "-" {
		t.Fatalf("silent idle reply line = %q, want a dash", reply)
	}
}

func TestTallRowTruncatesALongPrompt(t *testing.T) {
	r := testRenderer()
	sess := Session{ID: "notes", Status: "idle", Prompt: strings.Repeat("triage the flaky integration suite and report ", 10)}
	lines := strings.Split(r.tallRow(sess, "head", "idle", "  ", false, 80, ""), "\n")
	for _, line := range lines {
		if got := ansi.StringWidth(line); got > 80 {
			t.Fatalf("row line is %d wide, row is 80:\n%s", got, ansi.Strip(line))
		}
	}
	if prompt := ansi.Strip(lines[1]); !strings.Contains(prompt, "…") {
		t.Fatalf("long prompt should truncate with an ellipsis:\n%s", prompt)
	}
	if head := ansi.Strip(lines[0]); !strings.Contains(head, "idle") {
		t.Fatalf("meta should survive the long prompt:\n%s", head)
	}
}
