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

func TestNavigationHeaderKeepsLiteralBranchesAndTargetHit(t *testing.T) {
	m := New(false)
	m.scope = git.ScopeBranch
	m.set = diff.Set{Repo: git.Repo{Root: "/repo", Branch: "feature→branch"}, BaseDesc: "base←"}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		text := ansi.Strip(m.Header(RenderContext{GlyphMode: mode}, "session ↑↓", 220))
		separator := " → "
		if mode == presentation.GlyphModeBasic {
			separator = " -> "
		}
		want := "base←" + separator + "feature→branch"
		if !strings.Contains(text, want) || !strings.Contains(text, "session ↑↓") {
			t.Fatalf("%s header changed literal text: %s", mode, text)
		}
		found := false
		for _, hit := range m.HeaderKeys() {
			if hit.Label == "B" {
				found = true
				if got := ansi.Cut(text, hit.X0, hit.X1); !strings.Contains(got, want) {
					t.Fatalf("target hit=%q", got)
				}
			}
		}
		if !found {
			t.Fatal("target pill lost mouse target")
		}
	}
}

func TestNavigationReviewOverflowKeepsLineMapsAndLiteralSource(t *testing.T) {
	const literal = "literal ↑↓←→"
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, split := range []bool{false, true} {
			m := New(split)
			m.target = Target{ID: "session"}
			m.scroll = 4
			fd := diff.FileDiff{File: git.ChangedFile{Path: "file.go"}, IsLoaded: true}
			for i := 0; i < 15; i++ {
				fd.Lines = append(fd.Lines, diff.Line{Kind: diff.Same, OldNum: i + 1, NewNum: i + 1, Text: literal, Pair: -1})
			}
			m.set = diff.Set{Files: []diff.FileDiff{fd}}
			text := ansi.Strip(m.Code(RenderContext{GlyphMode: mode}, 100, 4).Text)
			above := "4 more above"
			below := "more below"
			if mode == presentation.GlyphModeAdvanced {
				above = "↑ 4 more"
				below = "↓"
			}
			if len(strings.Split(text, "\n")) != 4 || !strings.Contains(text, above) || !strings.Contains(text, below) || !strings.Contains(text, literal) {
				t.Fatalf("%s/%t code overflow: %s", mode, split, text)
			}
			if line, ok := m.codeRows.At(0); !ok || line != 3 {
				t.Fatalf("overflow lost previous-line target: %d/%t", line, ok)
			}
		}
		m := New(false)
		m.target = Target{ID: "session"}
		m.fileIdx = 5
		for i := 0; i < 10; i++ {
			m.set.Files = append(m.set.Files, diff.FileDiff{File: git.ChangedFile{Path: literal}, IsLoaded: true})
		}
		text := ansi.Strip(m.FileList(RenderContext{GlyphMode: mode}, 80, 4).Text)
		if !strings.Contains(text, literal) {
			t.Fatal("file name changed")
		}
		if mode == presentation.GlyphModeBasic && (!strings.Contains(text, "more above") || !strings.Contains(text, "more below")) {
			t.Fatalf("file overflow: %s", text)
		}
		if _, ok := m.fileRows.At(0); !ok {
			t.Fatal("file overflow lost mouse target")
		}
	}
}
