package rail

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestContentMarksPreserveQuotedTextForEveryTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	const text = "❯↳⌕⚙⚠✔✖↓■ 界e\u0301"
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		r := renderer{ctx: RenderContext{GlyphMode: mode, Theme: theme.Palettes[0]}, palette: newPalette(theme.Palettes[0])}
		prompt, reply := "> ", "< "
		if mode == presentation.GlyphModeAdvanced {
			prompt, reply = "❯ ", "↳ "
		}
		for toolName, tool := range cfg.Tools {
			sess := Session{ID: "quoted", Tool: toolName, Status: "idle", IsShell: tool.Shell, Prompt: text, PaneLine: text}

			got := ansi.Strip(r.replyCell(sess, r.palette.subtle, 200))
			if got != reply+text {
				t.Fatalf("%s/%s reply = %q", mode, toolName, got)
			}
			sess.PaneLine = ""
			if got := ansi.Strip(r.compactCell(sess, r.palette.subtle, 200)); got != prompt+text {
				t.Fatalf("%s/%s prompt = %q", mode, toolName, got)
			}
			sess.PaneLine = text
			tall := strings.Split(ansi.Strip(r.tallRow(sess, "head", "", "  ", false, 200, "")), "\n")
			wantRows := 3
			if tool.Shell {
				wantRows = 2
			}
			if len(tall) != wantRows {
				t.Fatalf("%s/%s has %d rows, want %d", mode, toolName, len(tall), wantRows)
			}
			if !tool.Shell && !strings.Contains(tall[1], prompt+text) {
				t.Fatalf("missing prompt: %q", tall[1])
			}
			for _, line := range tall {
				if ansi.StringWidth(line) != 200 {
					t.Fatalf("wrong row width: %q", line)
				}
			}
		}
	}
}
