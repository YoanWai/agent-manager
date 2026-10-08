package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func storeReviewKey(t *testing.T, h *sessionHarness, spec string) {
	t.Helper()
	key, err := keybind.Parse(spec)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetKeys(keybind.DefaultSession().With(keybind.Review, keybind.Keys(key))); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedBackendReloadsBindingsBeforeCreatingATerminal(t *testing.T) {
	h := newSessionHarness(t)
	storeReviewKey(t, h, "ctrl+g")
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	backend := &Backend{configDir: h.sessions.configDir, owned: true, opened: true, runtime: Runtime{
		Config: cfg, Store: h.store, Driver: h.driver, Hooks: hooks.NewManager(h.sessions.configDir), Snapshot: h.store.SetSnapshot,
	}}
	sessions := NewSessionsWithBackend(backend, MCPVocabulary())
	if _, err := sessions.List(h.caller.ID); err != nil {
		t.Fatal(err)
	}
	storeReviewKey(t, h, "alt+g")
	terminals := NewTerminalsWithBackend(backend, MCPVocabulary())
	if _, err := terminals.Create(h.caller.ID, CreateTerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	bound, err := exec.Command("tmux", "-L", h.driver.SocketName(), "list-keys", "-T", "root").CombinedOutput()
	if err != nil {
		t.Fatalf("list-keys: %v: %s", err, bound)
	}
	newBinding, oldBinding := false, false
	for _, line := range strings.Split(string(bound), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.Contains(line, tmux.RequestReview) {
			continue
		}
		newBinding = newBinding || fields[3] == "M-g"
		oldBinding = oldBinding || fields[3] == "C-g"
	}
	if !newBinding || oldBinding {
		t.Fatalf("backend reinstalled stale bindings:\n%s", bound)
	}
}

func TestOwnedBackendCanOpenAfterItsStoreIsRepaired(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := filepath.Join(t.TempDir(), "agent-manager")
	if err := os.WriteFile(dir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	backend := OpenBackend(dir)
	defer backend.Close()
	if _, err := backend.Lifecycle(); err == nil {
		t.Fatal("a config directory that is a file was accepted")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Lifecycle(); err != nil {
		t.Fatalf("backend retained the repaired open error: %v", err)
	}
}

func TestBorrowedBackendReadsCoordinationFromItsStore(t *testing.T) {
	h := newSessionHarness(t)
	cfg, err := config.Default()
	if err != nil {
		t.Fatal(err)
	}
	backend, err := BorrowBackend(Runtime{
		Config: cfg, Store: h.store, Driver: h.driver,
		Hooks: hooks.NewManager(h.sessions.configDir), Snapshot: h.store.SetSnapshot,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proactive, err := backend.ProactiveCoordination(); err != nil || proactive {
		t.Fatalf("default coordination = %v, %v; want on request", proactive, err)
	}
	if err := h.store.SetProactiveCoordination(true); err != nil {
		t.Fatal(err)
	}
	if proactive, err := backend.ProactiveCoordination(); err != nil || !proactive {
		t.Fatalf("stored coordination = %v, %v; want proactive", proactive, err)
	}
}
