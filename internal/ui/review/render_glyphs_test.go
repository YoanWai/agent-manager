package review

import (
	"errors"
	"strings"
	"testing"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestDiffOutcomeMarksFollowMode(t *testing.T) {
	for _, tc := range []struct {
		mode                         presentation.GlyphMode
		failed, clean, done, pending string
	}{
		{presentation.GlyphModeBasic, "x", "+", "+", "."},
		{presentation.GlyphModeAdvanced, "✖", "✓", "✔", "○"},
	} {
		m := New(false)
		m.target = Target{ID: "test"}
		ctx := RenderContext{GlyphMode: tc.mode}
		r := m.renderer(ctx)
		if got := ansi.Strip(r.emptyText()); !strings.HasPrefix(got, tc.clean+" no changes") {
			t.Fatalf("%s clean=%q", tc.mode, got)
		}
		m.errText = "⚠ literal error"
		if got := ansi.Strip(r.emptyText()); got != tc.failed+" ⚠ literal error" {
			t.Fatalf("%s error=%q", tc.mode, got)
		}
		m.errText = ""
		if got := ansi.Strip(r.bodyNote(&diff.FileDiff{Err: errors.New("⚠ literal error")})); got != tc.failed+" ⚠ literal error" {
			t.Fatalf("%s file error=%q", tc.mode, got)
		}
		m.notice = "✔ literal outcome"
		if got := strings.TrimSpace(ansi.Strip(m.Status(ctx, 80))); got != tc.done+" ✔ literal outcome" {
			t.Fatalf("%s status=%q", tc.mode, got)
		}
		m.notice = ""
		m.set.Files = []diff.FileDiff{{File: git.ChangedFile{Path: "file.go"}}}
		if got := ansi.Strip(m.FileList(ctx, 40, 10).Text); !strings.Contains(got, tc.pending+" file.go") {
			t.Fatalf("%s pending=%q", tc.mode, got)
		}
		m.reviewed = map[string]map[string]uint64{m.reviewKey(): {m.markKey("file.go"): ContentHash(&m.set.Files[0])}}
		if got := ansi.Strip(m.FileList(ctx, 40, 10).Text); !strings.Contains(got, tc.done+" file.go") {
			t.Fatalf("%s done=%q", tc.mode, got)
		}
	}
}

func TestCompatibleSendConfirmationMeasuresDisplayedKeys(t *testing.T) {
	m := New(false)
	m.sendConfirm = true
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{40, 60, 120} {
			text := ansi.Strip(m.Status(RenderContext{GlyphMode: mode}, width))
			for _, hit := range m.StatusKeys() {
				got := ansi.Cut(text, hit.X0, hit.X1)
				want := presentation.KeyLabel(mode, hit.Label)
				if !strings.HasPrefix(got, want) {
					t.Fatalf("%s/%d %q hit=%q", mode, width, hit.Label, got)
				}
			}
			if mode == presentation.GlyphModeBasic && strings.ContainsAny(text, "↵⌫") {
				t.Fatalf("compatible status=%q", text)
			}
		}
	}
}
