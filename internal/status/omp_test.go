package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ompFrame(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "omp", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// omp 18.2.11 fixtures are captures of the real TUI in its default band
// composer, driven against a local OpenAI-compatible stub: the first-run
// splash and setup wizard, a fresh launch, streamed and finished turns, a
// tool approval dialog, a reply ending in a question, a provider error, a
// rate-limit retry, and a resumed session.
func TestOmpPanes(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct{ frame, want string }{
		{"splash", Waiting},
		{"setup-wizard", Waiting},
		{"launched", Finished},
		{"resting-draft", Finished},
		{"working", Working},
		{"finished", Finished},
		{"finished-slow", Finished},
		{"approval", Waiting},
		{"after-approve", Finished},
		{"question", Waiting},
		{"errored", Errored},
		{"retrying", Working},
		{"retrying-after-error", Working},
		{"resumed", Finished},
	} {
		t.Run(tc.frame, func(t *testing.T) {
			if got, _ := engine.Match("omp", ompFrame(t, tc.frame)); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.frame, got, tc.want)
			}
		})
	}
}

// omp 18.4.2 fixtures are captures of the same flows on 18.4.2, which
// draws its key hints as glyphs (⏎, ⎋, ↑/↓) where 18.2.11 spelled them
// out: the splash, the setup wizard and the tool approval footer.
func TestOmp1842Panes(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct{ frame, want string }{
		{"splash", Waiting},
		{"setup-wizard", Waiting},
		{"launched", Finished},
		{"working", Working},
		{"finished", Finished},
		{"approval", Waiting},
		{"after-approve", Finished},
		{"question", Waiting},
	} {
		t.Run(tc.frame, func(t *testing.T) {
			if got, _ := engine.Match("omp", ompFrame(t, "18.4.2/"+tc.frame)); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.frame, got, tc.want)
			}
		})
	}
}

// Shapes derived from the captures: a question or an error that a later
// turn answered, and a draft typed into the composer's gutter row.
func TestOmpPanesAcrossTurns(t *testing.T) {
	engine := defaultEngine(t)
	question := ompFrame(t, "question")
	answered := strings.Replace(question, " I can do that. Which file should I edit?\n",
		" I can do that. Which file should I edit?\n\n\n main.go\n\n\n Edited main.go.\n", 1)
	errored := ompFrame(t, "errored")
	recovered := strings.Replace(errored, " Dismissed when you send your next message.\n"+strings.Repeat("─", 110)+"\n",
		" Dismissed when you send your next message.\n"+strings.Repeat("─", 110)+"\n\n\n try again\n\n\n Done.\n", 1)
	if recovered == errored {
		t.Fatal("errored fixture lost its dismissable error box")
	}
	withDraft := func(pane, draft string) string {
		i := strings.LastIndex(pane, "\n╰─")
		return pane[:i] + "\n╰─ " + draft + strings.TrimPrefix(pane[i:], "\n╰─")
	}
	for _, tc := range []struct {
		name, pane, want string
	}{
		{"answered question", answered, Finished},
		{"error followed by a turn", recovered, Finished},
		{"draft under a running turn", withDraft(ompFrame(t, "working"), "and then the tests"), Working},
		{"draft answering a question", withDraft(question, "main.go"), Waiting},
		{"draft under an error", withDraft(errored, "retry"), Errored},
		{"draft ending in a question mark", withDraft(ompFrame(t, "finished"), "why?"), Finished},
		{"wrapped draft under a running turn", withDraft(ompFrame(t, "working"), "and then the tests\n   in the retry path"), Working},
		{"wrapped draft answering a question", withDraft(question, "main.go, and the\n   retry path too"), Waiting},
		{"wrapped draft under an error", withDraft(errored, "retry the\n   last step"), Errored},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, _ := engine.Match("omp", tc.pane); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.name, got, tc.want)
			}
		})
	}
}

func TestOmpComposerRow(t *testing.T) {
	engine := defaultEngine(t)
	if !engine.MatchesActivityCutoff("omp", "╰─ a draft") {
		t.Fatal("the gutter row with a draft is not the input row")
	}
	if !engine.MatchesActivityCutoff("omp", "╰─") {
		t.Fatal("the empty gutter row is not the input row")
	}
	if engine.MatchesActivityCutoff("omp", "╰────────────────────╯") {
		t.Fatal("a tool box's bottom border reads as the input row")
	}
}
