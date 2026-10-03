package ui

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

func TestRowMarksFitOneCellWithoutFontFallback(t *testing.T) {
	marks := []string{workingGlyph, startingGlyph, waitingGlyph, finishedGlyph, erroredGlyph, idleGlyph,
		reorderGrip, inboxGlyph, branchGlyph, groupOpenGlyph, groupClosedGlyph}
	marks = append(marks, startupFrames[:]...)
	for _, mark := range marks {
		if width := ansi.StringWidth(mark); width != 1 {
			t.Errorf("mark %q occupies %d cells, want 1", mark, width)
		}
		for _, r := range mark {
			if r < '!' || r > '~' {
				t.Errorf("mark %q needs a character outside printable ASCII", mark)
			}
		}
	}
}

func TestStatusMarksRemainDistinctWithoutColor(t *testing.T) {
	seen := map[string]string{}
	for _, state := range []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Idle} {
		mark := statusGlyph(state)
		if other, ok := seen[mark]; ok {
			t.Errorf("%s and %s share %q", other, state, mark)
		}
		seen[mark] = state
	}
	if statusGlyph(status.Dead) != statusGlyph(status.Errored) {
		t.Fatal("dead sessions lost their error mark")
	}
}

func TestRowStatusMarksCoverEveryBuiltinTool(t *testing.T) {
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	for toolName := range cfg.Tools {
		t.Run(toolName, func(t *testing.T) {
			m := &Model{cfg: cfg}
			for _, state := range []string{status.Working, status.Starting, status.Waiting, status.Finished, status.Errored, status.Dead, status.Idle} {
				sess := store.Session{Tool: toolName, Status: state}
				for phase := 0; phase < len(startupFrames)*2; phase++ {
					m.startupPhase = phase
					want := statusGlyph(state)
					if state == status.Starting {
						want = startupFrames[phase%len(startupFrames)]
					} else if cfg.Tools[toolName].Shell && state != status.Errored && state != status.Dead {
						want = shellGlyph
					}
					if got := ansi.Strip(m.sessionGlyph(sess)); got != want {
						t.Errorf("%s phase %d: got %q, want %q", state, phase, got, want)
					}
				}
			}
		})
	}
}
