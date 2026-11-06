package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func kiroFrame(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "kiro", "3.1.0", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// A closed turn rests on the default. The poller turns a working turn that
// goes quiet into finished.
func TestKiroPanes(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct{ frame, want string }{
		{"startup", Waiting},
		{"positional-trust", Waiting},
		{"idle", Idle},
		{"idle-narrow", Idle},
		{"draft", Idle},
		{"working", Working},
		{"working-thinking", Working},
		{"approval", Waiting},
		{"picker", Waiting},
		{"resume-picker", Waiting},
		{"finished", Idle},
		{"positional-done", Idle},
		{"resumed", Idle},
	} {
		t.Run(tc.frame, func(t *testing.T) {
			if got, _ := engine.Match("kiro", kiroFrame(t, tc.frame)); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.frame, got, tc.want)
			}
		})
	}
}

func TestKiroDialogsHoldTyping(t *testing.T) {
	engine := defaultEngine(t)
	for frame, want := range map[string]string{"startup": Waiting, "approval": Waiting, "picker": Waiting, "resume-picker": Waiting, "working": Working, "idle": "", "finished": ""} {
		if got := engine.TypingHold("kiro", kiroFrame(t, frame)); got != want {
			t.Fatalf("TypingHold(%s) = %q want %q", frame, got, want)
		}
	}
}

func TestKiroComposer(t *testing.T) {
	engine := defaultEngine(t)
	for _, frame := range []string{"idle", "finished", "working"} {
		if draft, ok := engine.InputDraft("kiro", kiroFrame(t, frame)); ok || draft != "" {
			t.Fatalf("%s draft = %q %v", frame, draft, ok)
		}
	}
	if draft, ok := engine.InputDraft("kiro", kiroFrame(t, "draft")); !ok || draft != "draft text not sent" {
		t.Fatalf("draft = %q %v", draft, ok)
	}
	if echoed, ok := engine.LastUserEcho("kiro", kiroFrame(t, "finished")); !ok || echoed != "Run the shell command: ls" {
		t.Fatalf("echo = %q %v", echoed, ok)
	}
	if got, _, ok := engine.LastMessage("kiro", kiroFrame(t, "positional-done")); !ok || got != "ok" {
		t.Fatalf("LastMessage = %q %v", got, ok)
	}
}

func TestKiroClosedTurnEndsOnItsReply(t *testing.T) {
	engine := defaultEngine(t)
	finished := kiroFrame(t, "finished")
	region, ok := engine.ActivityRegion("kiro", finished)
	if !ok {
		t.Fatal("no activity region at the idle composer")
	}
	if got := engine.TurnEndedState("kiro", region); got != Finished {
		t.Fatalf("TurnEndedState = %q want finished", got)
	}
	asked := strings.Replace(region, "• README.md\n", "• Want me to open README.md?\n", 1)
	if got := engine.TurnEndedState("kiro", asked); got != Waiting {
		t.Fatalf("TurnEndedState after a question = %q want waiting", got)
	}
	// The status bar's context gauge moves between turns. It is chrome, so it
	// never reads as streaming output.
	moved := strings.Replace(region, "◔ 2%", "◔ 9%", 1)
	if engine.RegionContent("kiro", moved) != engine.RegionContent("kiro", region) {
		t.Fatal("a moved context gauge changed the region content")
	}
}

func TestKiroQuotedDialogsAndBusyRowStayIdle(t *testing.T) {
	engine := defaultEngine(t)
	finished := kiroFrame(t, "finished")
	for _, quote := range []string{
		" Trust this workspace?\n ❯ Trust, continue\n esc to exit · ↑↓ to navigate · ↵ to select",
		" Run ls requires approval\n ❯ Allow once\n esc to close · ↑↓ to navigate · ↵ to select · Tab to edit",
		"›  Kiro is working · 3s · Type to steer · Ctrl+S to queue",
		" ↑↓ to navigate · ↵ to select · ctrl+p to preview · + to view all controls",
	} {
		pane := strings.Replace(finished, "• README.md\n", quote+"\n", 1)
		if got, _ := engine.Match("kiro", pane); got != Idle {
			t.Fatalf("quote %q = %q want idle", quote, got)
		}
	}
}

func TestKiroQuotedPickersStayFinished(t *testing.T) {
	engine := defaultEngine(t)
	for _, frame := range []string{"picker", "resume-picker"} {
		t.Run(frame, func(t *testing.T) {
			quote := "• Here is a quoted picker:\n" + kiroFrame(t, frame)
			pane := strings.Replace(kiroFrame(t, "finished"), "• README.md\n", quote, 1)
			if got, _ := engine.Match("kiro", pane); got != Idle {
				t.Fatalf("quoted picker = %q want idle", got)
			}
			if got := engine.TypingHold("kiro", pane); got != "" {
				t.Fatalf("quoted picker typing hold = %q want none", got)
			}
			region, ok := engine.ActivityRegion("kiro", pane)
			if !ok || engine.TurnEndedState("kiro", region) != Finished {
				t.Fatal("quoted picker did not settle finished")
			}
		})
	}
}
