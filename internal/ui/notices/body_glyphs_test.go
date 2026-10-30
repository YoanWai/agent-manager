package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleNoticeChromeKeepsBodyAndScrollGeometry(t *testing.T) {
	const literal = "◂▸╭╮╰╯━╸┃┄▀▄▐▜▟"
	n := Notice{Headline: "Headline", Body: []string{literal}}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		rows := renderBody(n, 80, testTheme(), mode)
		plain := ansi.Strip(strings.Join(rows, "\n"))
		if !strings.Contains(plain, literal) {
			t.Fatal("notice body was converted")
		}
		rule, thumb := "─", "█"
		if mode == presentation.GlyphModeAdvanced {
			rule, thumb = "━", "┃"
		}
		if ansi.Strip(rows[1]) != strings.Repeat(rule, len("Headline")) {
			t.Fatalf("notice rule=%q", rows[1])
		}
		body := make([]string, 12)
		for i := range body {
			body[i] = "text"
		}
		for _, offset := range []int{0, 4, 8} {
			window := scrollWindow(body, 4, offset, 20, testTheme(), mode)
			if len(window) != 4 {
				t.Fatal("scroll height changed")
			}
			count := 0
			for _, row := range window {
				if ansi.StringWidth(row) != 20 {
					t.Fatal("scroll width changed")
				}
				if strings.HasSuffix(ansi.Strip(row), thumb) {
					count++
				}
			}
			if count != 1 {
				t.Fatalf("offset %d has %d thumbs", offset, count)
			}
		}
	}
}
