package status

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
)

// Tail keeps a pane's newest content rows: the composer, its footer, turn
// summaries and whatever else the tool's chrome rules mark are frame, and
// blanks do not count. A prompt echo stays unless those rules mark it, as
// claude's chrome_block does. Panes are the shapes the other
// tests in this package pinned against live sessions.
func TestTailPerTool(t *testing.T) {
	engine := defaultEngine(t)
	fixture := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	for _, tc := range []struct {
		tool, pane string
		n          int
		want       string
	}{
		{"claude", "● Ran the suite.\n\n● Done. The fix is in auth.go.\n  Two tests were touched.\n\n✻ Cerebrating… (4s · esc to interrupt)\n\n❯ \n  ? for shortcuts", 2,
			"● Done. The fix is in auth.go.\n  Two tests were touched."},
		{"opencode", "  ┃\n  ┃  Reply with exactly: OPENCODE ECHO TEST DONE.\n  ┃\n     OPENCODE ECHO TEST DONE.\n     Build · Gemini 3.6 Flash · 2.6s · 3.0 tok/s\n  ┃\n  ┃\n  ┃  Build · Gemini 3.6 Flash Google\n  ╹▀▀▀▀▀▀▀▀▀▀▀▀", 1,
			"     OPENCODE ECHO TEST DONE."},
		{"codex", "› ASK me\n• Which one do you want?\n  02:42\n                          Tip: Start a fresh idea with /new; the previous session stays in history.\n› Ask Codex to do anything\n  gpt-5.1-codex default · /private/tmp/am586/work", 5,
			"› ASK me\n• Which one do you want?"},
		{"muse", "❯ earlier prompt\n◆ earlier reply\n❯ hello muse\n\n◆ echo: hello muse\n\n────────────────\n❯\n────────────────\n  echo · /work · Auto-review\n", 2,
			"❯ hello muse\n◆ echo: hello muse"},
		{"grok", fixture("testdata/grok/1.0.50/minimal-two-prompts.txt"), 2, "❯ Now reply with exactly the word YES.\nYES"},
		{"gemini", " > Reply with exactly: GEMINI ECHO TEST DONE.\n▀▀▀▀▀▀▀▀▀▀▀▀\n✦ GEMINI ECHO TEST DONE.\n                  ? for shortcuts\n────────────\n Shift+Tab to accept edits\n▄▄▄▄▄▄▄▄▄▄▄▄\n >   Type your message or @path/to/file\n▀▀▀▀▀▀▀▀▀▀▀▀", 1,
			"✦ GEMINI ECHO TEST DONE."},
		{"antigravity", agyPane("> Run sleep 6.\n\n● Bash(sleep 6) (ctrl+o to expand)\n\n  The sleep 6 command has finished executing.\n\n", ">", "? for shortcuts"), 2,
			"● Bash(sleep 6) (ctrl+o to expand)\n  The sleep 6 command has finished executing."},
		{"hermes", "╭────╮\n│ All done. │\n╰────╯\n  ⚕ hermes-4 │ ctx -- │ ⏲ 4s\n────────\n❯ \n", 3, "│ All done. │"},
		{"terminal", "$ go build ./...\n$ go test ./...\nok  \tpkg\t0.1s\n$ \n\n\n", 2, "ok  \tpkg\t0.1s\n$"},
		{"pi", "  a pi reply row\n────────────\n\n────────────\n/tmp (main)\n", 3, "  a pi reply row"},
		{"omp", fixture("testdata/omp/question.txt"), 1, " I can do that. Which file should I edit?"},
		{"command-code", "# Command Code v1.32.1\n❯ Reply with exactly: CMD ECHO TEST DONE.\n⠶ CMD ECHO TEST DONE.\n  And a second line of the reply.\n────────────────────────\n❯ Ask your question...\n────────────────────────\n  ? for shortcuts · taste on", 2,
			"⠶ CMD ECHO TEST DONE.\n  And a second line of the reply."},
		{"cursor", fixture("testdata/cursor/2026.10.19-6bbf2c4/resting.txt"), 1, "  Done — pong.txt now contains pong."},
		{"kiro", fixture("testdata/kiro/3.1.0/finished.txt"), 2, "    README.md\n• README.md"},
	} {
		if got := engine.Tail(tc.tool, tc.pane, tc.n); got != tc.want {
			t.Errorf("%s: Tail = %q, want %q", tc.tool, got, tc.want)
		}
	}
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	for tool := range cfg.Tools {
		if !slices.Contains([]string{"claude", "opencode", "codex", "muse", "grok", "gemini", "antigravity", "hermes", "terminal", "pi", "omp", "command-code", "cursor", "kiro"}, tool) {
			t.Errorf("no Tail case for %s", tool)
		}
	}
}

// A dialog draws below the cutoff, or on it, so the rows an orchestrator
// answers from would fall outside the activity region without the waiting
// rule that keeps them.
func TestTailKeepsAnOpenDialog(t *testing.T) {
	engine := defaultEngine(t)
	ask := "⏺ Tea, good choice.\n\n❯ Ask me cats or dogs.\n────────────\n ☐ Pet pref\n\nDo you prefer cats or dogs?\n\n" +
		"❯ 1. Cats\n     You prefer cats\n  2. Dogs\n     You prefer dogs\n\nEnter to select · ↑/↓ to navigate · Esc to cancel"
	if got, want := engine.Tail("claude", ask, 6), "Do you prefer cats or dogs?\n❯ 1. Cats\n     You prefer cats\n  2. Dogs\n     You prefer dogs\nEnter to select · ↑/↓ to navigate · Esc to cancel"; got != want {
		t.Errorf("claude question: Tail = %q, want %q", got, want)
	}
	permission := "⏺ Ran.\n\n✻ Crunched for 8s · done 23:37\n\n❯ Use the Write tool to create /etc/amhk-probe.txt.\n\n  Writing /etc/amhk-probe.txt\n────────────\n" +
		" Do you want to create amhk-probe.txt?\n ❯ 1. Yes\n   2. No\n\n Esc to cancel · Tab to amend\n"
	if got, want := engine.Tail("claude", permission, 5), "  Writing /etc/amhk-probe.txt\n Do you want to create amhk-probe.txt?\n ❯ 1. Yes\n   2. No\n Esc to cancel · Tab to amend"; got != want {
		t.Errorf("claude permission: Tail = %q, want %q", got, want)
	}
	// Codex marks the selected option with its prompt marker. Frame from a
	// live codex session on 2026-11-04.
	hooks := "  Hooks need review\n  14 hooks are new or changed.\n\n\n› 1. Review hooks\n  2. Trust all and continue\n\n  enter confirm · esc skip\n"
	if got, want := engine.Tail("codex", hooks, 3), "› 1. Review hooks\n  2. Trust all and continue\n  enter confirm · esc skip"; got != want {
		t.Errorf("codex hooks review: Tail = %q, want %q", got, want)
	}
	data, err := os.ReadFile("testdata/cursor/2026.10.19-6bbf2c4/approval.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Tail("cursor", string(data), 4); !strings.HasPrefix(got, "  → Run (once) (y)\n") {
		t.Errorf("cursor approval: Tail = %q, want the selected option kept", got)
	}
	data, err = os.ReadFile("testdata/kiro/3.1.0/approval.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Tail("kiro", string(data), 6); !strings.HasPrefix(got, " shell → ls\n ❯ Allow once\n") {
		t.Errorf("kiro approval: Tail = %q, want the question and its options kept", got)
	}
}
