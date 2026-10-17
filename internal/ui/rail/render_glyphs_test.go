package rail

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestGlyphHandleGeometry(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, comfortable := range []bool{false, true} {
			for _, group := range []bool{false, true} {
				for _, depth := range []int{0, 1, 2, 8} {
					for _, width := range []int{8, 9, 10, 11, 12, 16, 24, 28, 44, 80} {
						t.Run(fmt.Sprintf("%s/%t/%t/%d/%d", mode, comfortable, group, depth, width), func(t *testing.T) {
							g := presentation.GlyphsFor(mode)
							row := treeRow{kind: SessionRow, depth: depth, sess: Session{ID: "one", Name: "界e\u0301=notes " + g.ReorderGrip, Status: "idle"}}
							if group {
								row.kind = GroupRow
								row.group = "界e\u0301=group " + g.ReorderGrip
							}
							r := renderer{model: Model{rows: []treeRow{row}}, ctx: RenderContext{GlyphMode: mode, Width: width, ComfortableRows: comfortable}, palette: newPalette(theme.Palettes[0]), frame: Frame{Handles: map[string]int{}}}
							lines := r.entryRowLines(row, 0, "")
							if x, ok := r.frame.Handles[row.key()]; ok {
								if got := ansi.Strip(ansi.Cut(lines[0].Text, x-1, x)); got != g.ReorderGrip {
									t.Fatalf("handle %d points at %q", x, got)
								}
								if x-1 >= width-menuButtonWidth {
									t.Fatal("handle overlaps menu")
								}
							} else if width == 80 {
								t.Fatal("wide row lost handle")
							}
							for _, line := range lines {
								if got := ansi.StringWidth(line.Text); got != width {
									t.Fatalf("row width %d, want %d", got, width)
								}
							}
						})
					}
				}
			}
		}
	}
}

func TestHiddenGlyphHandlesAndRenamedGroups(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, group := range []string{"", "parent/界=child"} {
			for _, collapsed := range []bool{false, true} {
				g := presentation.GlyphsFor(mode)
				row := treeRow{kind: GroupRow, group: group}
				model := Model{rows: []treeRow{row}, collapsed: map[string]bool{group: collapsed}}
				r := renderer{model: model, ctx: RenderContext{GlyphMode: mode, Width: 80}, palette: newPalette(theme.Palettes[0]), frame: Frame{Handles: map[string]int{}}}
				before := r.renderTreeRow(row, true, 80, 0, "")
				r.ctx.Rename = Rename{Target: row.selection(), View: "rename " + g.ReorderGrip + " = 界"}
				after := r.renderTreeRow(row, true, 80, 0, "")
				want := g.GroupOpenGlyph
				if collapsed {
					want = g.GroupClosedGlyph
				}
				if group == "" {
					want = " "
				}
				for _, text := range []string{before.text, after.text} {
					if got := ansi.Strip(ansi.Cut(text, railInset, railInset+1)); got != want {
						t.Fatalf("group mark %q, want %q", got, want)
					}
				}
				r.entryRowLines(row, 0, "")
				if len(r.frame.Handles) != 0 {
					t.Fatal("rename text created a handle")
				}
				row = treeRow{kind: SessionRow, sess: Session{ID: "one", Name: "name " + g.ReorderGrip + " = 界"}}
				r.ctx.Rename = Rename{Target: row.selection(), View: "rename " + g.ReorderGrip + " = 界"}
				r.entryRowLines(row, 0, "")
				if len(r.frame.Handles) != 0 {
					t.Fatal("session rename text created a handle")
				}
				r.ctx.Rename = Rename{}
				r.ctx.MouseDisabled = true
				r.entryRowLines(row, 0, "")
				if len(r.frame.Handles) != 0 {
					t.Fatal("mouse-disabled text created a handle")
				}
			}
		}
	}
}

func TestPortableGlyphsPreserveUserTextAndSpinnerGeometry(t *testing.T) {
	const text = "◐◌◆●○✕ ⠿ ✉ ⑂ ⠋ 界e\u0301"
	const width = 180
	for _, palette := range theme.Palettes {
		for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
			g := presentation.GlyphsFor(mode)
			working := treeRow{kind: SessionRow, sess: Session{ID: "one", Name: text, Status: "working", Queued: 12}}
			r := renderer{model: Model{rows: []treeRow{working}}, ctx: RenderContext{GlyphMode: mode, Width: width}, palette: newPalette(palette), frame: Frame{Handles: map[string]int{}}}
			if head := strings.TrimSpace(ansi.Strip(r.entryRowLines(working, 0, "")[0].Text)); !strings.HasPrefix(head, g.WorkingGlyph+" "+g.ReorderGrip+" ") {
				t.Fatalf("%s/%s row lost its state mark or handle: %q", palette.Name, mode, head)
			}
			for _, comfortable := range []bool{false, true} {
				row := treeRow{kind: SessionRow, sess: Session{ID: "one", Name: text, Prompt: "prompt " + text, PaneLine: "reply " + text, Status: "starting", Queued: 12}}
				r := renderer{model: Model{rows: []treeRow{row}}, ctx: RenderContext{GlyphMode: mode, Width: width, ComfortableRows: comfortable}, palette: newPalette(palette), frame: Frame{Handles: map[string]int{}}}
				want := []string{text}
				if comfortable {
					want = append(want, "prompt "+text, "reply "+text)
				}
				nameX, handleX := -1, -1
				for phase := 0; phase < len(g.StartupFrames)*3; phase++ {
					r.ctx.StartupPhase = phase
					lines := r.entryRowLines(row, 0, "")
					for i, text := range want {
						if !strings.Contains(ansi.Strip(lines[i].Text), text) {
							t.Fatalf("%s/%s line %d lost %q", palette.Name, mode, i, text)
						}
					}
					for _, line := range lines {
						if got := ansi.StringWidth(line.Text); got != width {
							t.Fatalf("%s/%s phase %d paints %d cells, want %d", palette.Name, mode, phase, got, width)
						}
					}
					head := ansi.Strip(lines[0].Text)
					x := ansi.StringWidth(head[:strings.Index(head, text)])
					handle, ok := r.frame.Handles[row.key()]
					if !ok {
						t.Fatalf("%s/%s phase %d lost the handle", palette.Name, mode, phase)
					}
					if phase == 0 {
						nameX, handleX = x, handle
					} else if nameX != x || handleX != handle {
						t.Fatal("spinner moved name or handle")
					}
					if !strings.Contains(head, g.InboxGlyph+"12") {
						t.Fatal("inbox glyph missing")
					}
				}
			}
		}
	}
}
