package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestOwnedBackendReloadsBindingsBeforeCreatingATerminal(t *testing.T) {
	h := newSessionHarness(t)
	initial := "[keybindings.session]\nreview = \"ctrl+g\"\n"
	if err := os.WriteFile(filepath.Join(h.sessions.configDir, "config.toml"), []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadDir(h.sessions.configDir)
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
	updated := strings.Replace(initial, `review = "ctrl+g"`, `review = "alt+g"`, 1)
	if err := os.WriteFile(filepath.Join(h.sessions.configDir, "config.toml"), []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
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

func TestOwnedBackendCanOpenAfterConfigurationIsRepaired(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("[tools"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := OpenBackend(dir)
	defer backend.Close()
	if _, err := backend.Lifecycle(); err == nil {
		t.Fatal("malformed configuration was accepted")
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Lifecycle(); err != nil {
		t.Fatalf("backend retained the repaired configuration error: %v", err)
	}
}
