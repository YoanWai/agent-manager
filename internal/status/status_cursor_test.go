package status

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func cursorFrame(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "cursor", "2026.10.19-6bbf2c4", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

func TestCursorPanes(t *testing.T) {
	engine := defaultEngine(t)
	for _, tc := range []struct{ frame, want string }{
		{"splash", Waiting},
		{"trust", Waiting},
		{"trusting", Waiting},
		{"idle", Idle},
		{"draft", Idle},
		{"working", Working},
		{"running", Working},
		{"approval", Waiting},
		{"resting", Idle},
	} {
		t.Run(tc.frame, func(t *testing.T) {
			if got, _ := engine.Match("cursor", cursorFrame(t, tc.frame)); got != tc.want {
				t.Fatalf("Match(%s) = %q want %q", tc.frame, got, tc.want)
			}
		})
	}
}

func TestCursorComposer(t *testing.T) {
	engine := defaultEngine(t)
	if draft, ok := engine.InputDraft("cursor", cursorFrame(t, "idle")); ok || draft != "" {
		t.Fatalf("idle draft = %q %v", draft, ok)
	}
	draft, ok := engine.InputDraft("cursor", cursorFrame(t, "draft"))
	if !ok || draft != "Reply with the single word pong. Do not use tools." {
		t.Fatalf("draft = %q %v", draft, ok)
	}
	if draft, ok := engine.InputDraft("cursor", cursorFrame(t, "running")); ok || draft != "" {
		t.Fatalf("running composer = %q %v", draft, ok)
	}
	got, _, ok := engine.LastMessage("cursor", cursorFrame(t, "resting"))
	if !ok || got != "Done — pong.txt now contains pong." {
		t.Fatalf("LastMessage = %q %v", got, ok)
	}
}

func TestCursorQuotedFooterStaysIdle(t *testing.T) {
	engine := defaultEngine(t)
	resting := cursorFrame(t, "resting")
	for _, quote := range []string{"ctrl+c to stop", "Run this command?"} {
		if got, _ := engine.Match("cursor", quote+"\n"+resting); got != Idle {
			t.Fatalf("quote %q = %q want idle", quote, got)
		}
	}
}
