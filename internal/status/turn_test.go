package status

import (
	"testing"
)

// Gemini closes turns without a summary line, so resting status comes from
// TurnEndedState over the quiet region. The "? for shortcuts" hint and the
// approval-mode banner sit above the composer; both must count as chrome
// or the hint's "?" would read every finished turn as a question.
func TestGeminiTurnEndedState(t *testing.T) {
	engine := defaultEngine(t)
	finishedRegion := "✦ Dark screen glows with text\n  Commands flow, stories unfold\n  Prompt waits, ready now\n Press Ctrl+O to show more lines of the last response\n                    ? for shortcuts\n──────────────────────────────\n Shift+Tab to accept edits\n▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄▄\n"
	if got := engine.TurnEndedState("gemini", finishedRegion); got != Finished {
		t.Fatalf("TurnEndedState(finished region) = %q want %q", got, Finished)
	}
	questionRegion := "✦ Should I refactor module A or module B?\n\n                    ? for shortcuts\n Shift+Tab to accept edits\n"
	if got := engine.TurnEndedState("gemini", questionRegion); got != Waiting {
		t.Fatalf("TurnEndedState(question region) = %q want %q", got, Waiting)
	}
}

func TestTurnEndedState(t *testing.T) {
	engine := defaultEngine(t)
	cases := []struct {
		name   string
		region string
		want   string
	}{
		{"plain response", "• Final response.\n\n", Finished},
		{"question response", "• Which file should I edit, A or B?\n\n", Waiting},
		{"question above trailing separator", "• Which file?\n\n────────────\n", Waiting},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.TurnEndedState("codex", tc.region); got != tc.want {
				t.Fatalf("TurnEndedState = %q want %q", got, tc.want)
			}
		})
	}
}
