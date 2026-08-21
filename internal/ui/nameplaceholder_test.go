package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// spawnUnnamed launches through the New Session form with the name left
// blank, which is what makes a spawn auto-named and sends the agent the
// directive to name itself.
func spawnUnnamed(t *testing.T, m *Model, prompt string) store.Session {
	t.Helper()
	m.openForm()
	m.form.name.SetValue("")
	m.form.dir.SetValue(t.TempDir())
	m.form.prompt.input.SetValue(prompt)
	m.form.toolIndex = 0
	if _, _ = m.submitForm(); m.mode != modeList {
		t.Fatalf("submit left mode=%v err=%q", m.mode, m.errBar.text)
	}
	rows := m.sessionRows()
	if len(rows) != 1 {
		t.Fatalf("session rows = %d, want the one just spawned", len(rows))
	}
	return rows[0]
}

func TestAWaitingRowIsNamedByThePromptItWasGiven(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "add cursor pagination to the sessions list endpoint")

	got := ansi.Strip(m.displayName(sess))
	if !strings.HasPrefix(got, "add cursor") {
		t.Fatalf("displayName = %q, want it to open with the prompt", got)
	}
}

func TestAWaitingRowNeverShowsTheRenameDirective(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "rate limit the public search route")

	got := ansi.Strip(m.displayName(sess))
	for _, leaked := range []string{"Run this exact", "agent-manager rename", "Other agent sessions"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("displayName = %q, leaked the launch directive %q", got, leaked)
		}
	}
}

func TestAWaitingRowSpawnedWithNoPromptKeepsThePlaceholder(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "")

	if got := ansi.Strip(m.displayName(sess)); got != namePlaceholder {
		t.Fatalf("displayName = %q, want the bare placeholder %q", got, namePlaceholder)
	}
}

func TestAWaitingRowKeepsThePromptToOneShortLine(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "split the staging workspace\nout of the prod state file\nand re-run the plan")

	got := ansi.Strip(m.displayName(sess))
	if strings.ContainsAny(got, "\n\t") {
		t.Fatalf("displayName = %q, want one line", got)
	}
	if width := ansi.StringWidth(got); width > placeholderPromptWidth {
		t.Fatalf("displayName = %q is %d wide, want at most %d", got, width, placeholderPromptWidth)
	}
}

func TestAPromptNamedRowFallsBackToItsGeneratedNamePastTheGrace(t *testing.T) {
	m := buildModel(t)
	sess := spawnUnnamed(t, m, "cache the session lookup for 30 seconds")

	sess.CreatedAt = time.Now().Add(-renameGrace - time.Second)
	got := ansi.Strip(m.displayName(sess))
	if got != sess.Name || !strings.HasPrefix(got, "claude-") {
		t.Fatalf("displayName past the grace = %q, want the generated name %q", got, sess.Name)
	}
}
