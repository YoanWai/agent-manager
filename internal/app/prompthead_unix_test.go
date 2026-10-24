//go:build unix

package app

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

// piPane paints pi's composer, a bare row between two rules, holding draft,
// and parks the caret after it the way pi does. The session row names pi,
// so the check reads it with pi's own rules.
func piPane(t *testing.T, driver *tmux.Driver, st *store.Store, id, draft string) {
	t.Helper()
	rule := strings.Repeat("─", 40)
	script := "printf '%s\\n%s\\n%s\\033[2;" + strconv.Itoa(len(draft)+1) + "H' " + tmux.ShellQuote(rule) + " " + tmux.ShellQuote(draft) + " " + tmux.ShellQuote(rule) + "; exec sleep 300"
	if err := driver.Create(id, t.TempDir(), script, nil, 80, 10); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	if err := st.CreateSession(store.Session{ID: id, Name: id, Tool: "pi", Cwd: "/tmp", Status: "idle", CreatedAt: time.Now(), LastStatusAt: time.Now()}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		out, _ := driver.CapturePane(id)
		if strings.Count(out, rule) == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("pane never painted the composer: %q", out)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Issue 850: pi's empty prompt in an attached session answers yes, so ←
// goes back; a draft before the caret answers no, so ← stays the agent's.
func TestPromptHeadReadsTheAttachedPane(t *testing.T) {
	if _, err := exec.LookPath(tmux.Binary); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := tmux.NewWithSocket("amapphead")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { exec.Command(tmux.Binary, "-L", "amapphead", "kill-server").Run() })
	profile := t.TempDir()
	st, err := store.Open(filepath.Join(profile, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	piPane(t, driver, st, "aa01", "")
	piPane(t, driver, st, "aa02", "draft")

	if err := PromptHead(profile, driver, []string{"am_aa01"}); err != nil {
		t.Fatalf("pi's empty prompt: %v, want the caret at its head", err)
	}
	if err := PromptHead(profile, driver, []string{"am_aa02"}); !errors.Is(err, ErrNotAtPromptHead) {
		t.Fatalf("pi's draft: %v, want ErrNotAtPromptHead", err)
	}
	for _, name := range []string{"am_ffff", "aa01", "user-session"} {
		if err := PromptHead(profile, driver, []string{name}); err == nil {
			t.Fatalf("%s answered yes; only a managed session the store knows can", name)
		}
	}
	if err := st.SetSetting(store.ArrowStepSetting, "off"); err != nil {
		t.Fatal(err)
	}
	if err := PromptHead(profile, driver, []string{"am_aa01"}); !errors.Is(err, ErrNotAtPromptHead) {
		t.Fatalf("arrow step off: %v, want ErrNotAtPromptHead", err)
	}
}
