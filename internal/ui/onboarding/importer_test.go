package onboarding

import (
	"slices"
	"strings"
	"testing"
)

func importStep(t *testing.T) Model {
	t.Helper()
	m := New(agents, defaults(), false)
	m.SetInstalled(map[string]bool{"claude": true, "codex": true})
	walkTo(t, &m, Import)
	return m
}

func TestImportStartsOnlyOnceACLIIsChecked(t *testing.T) {
	m := importStep(t)
	if got := m.importSources(); !slices.Equal(got, []string{"claude", "codex"}) {
		t.Fatalf("sources = %v, want the installed agents", got)
	}
	if out := m.Key(press("enter")); out.Action != Stay || m.Step() != Import {
		t.Fatalf("enter with nothing checked = %+v", out)
	}

	m = importStep(t)
	m.Key(press("down"))
	m.Key(press("space"))
	m.Key(press("down"))
	m.Key(press("right"))
	if m.Step() != Import {
		t.Fatalf("→ on the days row left the step for %v", m.Step())
	}
	m.Key(press("down"))
	m.Key(press("right"))
	out := m.Key(press("enter"))
	want := ImportRequest{From: []string{"codex"}, Days: 14, Runner: "codex"}
	if out.Action != StartImport || !slices.Equal(out.Import.From, want.From) || out.Import.Days != want.Days || out.Import.Runner != want.Runner {
		t.Fatalf("enter = %+v, want %+v", out, want)
	}
}

func TestOnlyInstalledCheckedAgentsRunTheImport(t *testing.T) {
	m := New(agents, Choices{Hidden: map[string]bool{"claude": true}}, false)
	m.SetInstalled(map[string]bool{"claude": true, "codex": true})
	if got := m.importRunners(); !slices.Equal(got, []string{"codex"}) {
		t.Fatalf("runners = %v, want codex alone", got)
	}
}

func TestImportRowsTakeTheMouse(t *testing.T) {
	m := importStep(t)
	frame := m.View(viewContext(120, 33))
	m.Click(rowOf(t, frame, "[ ] claude"))
	m.Click(rowOf(t, frame, "Going back"))
	request, ok := m.pendingImport()
	if !ok || !slices.Equal(request.From, []string{"claude"}) || request.Days != 14 {
		t.Fatalf("after two clicks the import is %+v, ready %v", request, ok)
	}
	if got := m.Hints(); !slices.Contains(got, [2]string{"↵", "import and start"}) {
		t.Fatalf("hints = %v", got)
	}
}

func TestImportPromptNamesWhatToBringIn(t *testing.T) {
	prompt := ImportPrompt(ImportRequest{From: []string{"claude", "codex", "gemini"}, Days: 30, Runner: "claude"})
	for _, want := range []string{"claude, codex and gemini", "last 30 days", "list_sessions", "agent_session_id", "create_session", "resume", "--resume"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt misses %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "\n") {
		t.Errorf("a multi-line prompt can stay unsent in a composer:\n%s", prompt)
	}
}
