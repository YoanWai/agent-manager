package status

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"testing"
)

// InputDraft reads what the user has typed after the composer marker, and
// refuses the placeholder wording a composer paints on its empty row.
func TestInputDraft(t *testing.T) {
	engine := defaultEngine(t)
	if draft, ok := engine.InputDraft("claude", "● Done.\n\n❯ fix the flaky test"); !ok || draft != "fix the flaky test" {
		t.Fatalf("claude draft = %q ok=%v", draft, ok)
	}
	if _, ok := engine.InputDraft("claude", "● Done.\n\n❯ "); ok {
		t.Fatal("empty composer should carry no draft")
	}
	for _, placeholder := range []string{
		"Press up to edit queued messages",
		"Press up to edit queued messages, Enter to send them immediately",
		"Press up to select a queued message to edit, or Enter to send them now",
		"Press up to select a queued message, then Enter to edit it",
	} {
		if _, ok := engine.InputDraft("claude", "● Done.\n\n❯ "+placeholder); ok {
			t.Fatalf("the queued composer's placeholder %q should not read as a draft", placeholder)
		}
	}
	if _, ok := engine.InputDraft("codex", "› Ask Codex to do anything\n  gpt-5.6-terra medium · /home/dev"); ok {
		t.Fatal("codex placeholder should not read as a draft")
	}
	if draft, ok := engine.InputDraft("codex", "› rename the flag\n  gpt-5.6-terra medium · /home/dev"); !ok || draft != "rename the flag" {
		t.Fatalf("codex draft = %q ok=%v", draft, ok)
	}
	// A gutter composer sits above its cutoff, so the text after the
	// cutoff match is the box's border fill, not what was typed — even
	// when a typed line is sitting right there in the gutter.
	opencode := "┃\n" +
		"┃ fix the flaky test\n" +
		"╹▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀▀\n" +
		" ■⬝⬝⬝⬝⬝⬝  esc interrupt"
	if _, ok := engine.InputDraft("opencode", opencode); ok {
		t.Fatal("a gutter composer's border fill must not read as a draft")
	}
}

// Command Code shapes, verified accountless on v1.32.1 with the inject
// stream: replies open on a static ⠶ row, prompts echo on ❯ like claude,
// and the composer paints "Ask your question..." on its empty row.
func TestCommandCodeRowShapes(t *testing.T) {
	engine := defaultEngine(t)
	pane := "# Command Code v1.32.1\n" +
		"❯ Reply with exactly: CMD ECHO TEST DONE.\n" +
		"⠶ CMD ECHO TEST DONE.\n" +
		"  And a second line of the reply.\n" +
		"────────────────────────\n" +
		"❯ Ask your question...\n" +
		"────────────────────────\n" +
		"  ? for shortcuts · taste on"
	if echoed, ok := engine.LastUserEcho("command-code", pane); !ok || echoed != "Reply with exactly: CMD ECHO TEST DONE." {
		t.Fatalf("command-code echo = %q ok=%v", echoed, ok)
	}
	line, anchored, ok := engine.LastMessage("command-code", pane)
	if !ok || !anchored || line != "CMD ECHO TEST DONE. And a second line of the reply." {
		t.Fatalf("command-code reply = %q anchored=%v ok=%v", line, anchored, ok)
	}
	if _, ok := engine.InputDraft("command-code", "⠶ Done.\n❯ Ask your question..."); ok {
		t.Fatal("the composer placeholder should not read as a draft")
	}
}

// A degenerate cutoff like ^ matches every row at zero width. InputPrefix
// refuses it for tools that did not declare a prefix, and the row-matcher
// behind MatchesActivityCutoff refuses it just the same, so neither door
// can stamp arbitrary rows as composer rows.
func TestDegenerateCutoffStampsNothing(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"degenerate": {Command: "x", ActivityCutoff: "^"},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if _, ok := engine.InputPrefix("degenerate", "any row at all"); ok {
		t.Fatal("a zero-width cutoff read as an input prefix")
	}
	if engine.MatchesActivityCutoff("degenerate", "any row at all") {
		t.Fatal("a zero-width cutoff read as a composer boundary")
	}
}

// An empty composer is the pristine placeholder or a bare marker, and the
// placeholder closes the row, so a draft that merely quotes it mid-text
// stays a draft. Row shapes measured live on command-code v1.33.0: the
// placeholder shows until the first prompt is typed, and a composer cleared
// afterwards paints "❯" with nothing after it for the rest of the session.
func TestComposerIsEmpty(t *testing.T) {
	engine, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"command-code": {
			ActivityCutoff:      `(?m)^❯`,
			ComposerPlaceholder: "Ask your question...",
		},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if !engine.ComposerIsEmpty("command-code", "❯ Ask your question...") {
		t.Fatal("the pristine composer's placeholder was not recognised")
	}
	for _, row := range []string{"❯", "❯ ", "❯   "} {
		if !engine.ComposerIsEmpty("command-code", row) {
			t.Fatalf("a cleared composer %q did not read as empty", row)
		}
	}
	if engine.ComposerIsEmpty("command-code", "❯ fix the Ask your question... bug") {
		t.Fatal("a draft quoting the placeholder read as empty")
	}
	if engine.ComposerIsEmpty("command-code", "❯ retry Ask your question...") {
		t.Fatal("a draft ending with the placeholder read as empty")
	}
	// A tool that declares no placeholder never takes the parked-caret
	// path, so a bare marker of its own is not empty for this purpose.
	plain, err := NewEngine(config.Config{Tools: map[string]config.Tool{
		"claude": {ActivityCutoff: `(?m)^❯`},
	}})
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	if plain.ComposerIsEmpty("claude", "❯ ") {
		t.Fatal("a tool without a declared placeholder took the parked-caret path")
	}
}

func TestAntigravityReplyReading(t *testing.T) {
	engine := defaultEngine(t)
	fresh := agyLogo + agyRule + "\n>\n" + agyRule + "\n" + agyFooter("? for shortcuts") + agyTail
	if got, _, ok := engine.LastMessage("antigravity", fresh); !ok || got != "" {
		t.Errorf("a fresh session quoted %q, want nothing past the logo", got)
	}

	finished := agyPane("> Run the shell command sleep 6 and then reply with one short sentence saying it finished.\n\n"+
		"▸ Thought for 3s, 362 tokens\n  Considering available tools, the command execution tool seems appropriate for running `sleep 6`.\n\n"+
		"● Bash(sleep 6) (ctrl+o to expand)\n\n  The sleep 6 command has finished executing.\n\n", ">", "? for shortcuts")
	if got, _, _ := engine.LastMessage("antigravity", finished); got != "The sleep 6 command has finished executing." {
		t.Errorf("LastMessage = %q", got)
	}
	if got, ok := engine.LastUserEcho("antigravity", finished); !ok || got != "Run the shell command sleep 6 and then reply with one short sentence saying it finished." {
		t.Errorf("LastUserEcho = %q, %v", got, ok)
	}
	if got, bounded, ok := engine.FullTurnText("antigravity", finished); !ok || !bounded || got != "● Bash(sleep 6) (ctrl+o to expand)\n\n  The sleep 6 command has finished executing." {
		t.Errorf("FullTurnText = %q, %v, %v; the thinking summary is not the reply", got, bounded, ok)
	}
	region, _ := engine.ActivityRegion("antigravity", finished)
	if got := engine.TurnEndedState("antigravity", region); got != Finished {
		t.Errorf("TurnEndedState = %q", got)
	}

	running := agyPane("> Count slowly from 1 to 30, one number per line.\n⣷  Generating...\n▸ Then say done.\n", ">", "  Press up to edit queued messages")
	if got, _, _ := engine.LastMessage("antigravity", running); got != "> Count slowly from 1 to 30, one number per line." {
		t.Errorf("LastMessage = %q, want the prompt over the spinner and the queued message", got)
	}

	interrupted := agyPane("> Write a 40 line poem about tmux.\n\n  ⎿  Interrupted · What should Antigravity CLI do instead?\n", ">", "? for shortcuts")
	region, _ = engine.ActivityRegion("antigravity", interrupted)
	if got := engine.TurnEndedState("antigravity", region); got != Waiting {
		t.Errorf("an interrupted turn settled as %q", got)
	}

	if got, ok := engine.InputDraft("antigravity", agyPane("", "> tidy the imports", "")); !ok || got != "tidy the imports" {
		t.Errorf("InputDraft = %q, %v", got, ok)
	}
	for _, placeholder := range []string{"> Accept-edits mode: file edits auto-approved (shift+tab to cycle)", "> Plan mode: research & plan only (shift+tab to cycle)"} {
		if got, ok := engine.InputDraft("antigravity", agyPane("", placeholder, "? for shortcuts")); ok {
			t.Errorf("mode placeholder read as draft %q", got)
		}
	}
}
