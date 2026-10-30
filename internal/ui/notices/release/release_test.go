package release

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/update"
)

func TestDelegatedResult(t *testing.T) {
	manager := update.Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	// A base name no PATH holds, so RestartTarget deterministically falls
	// back to execPath on any machine.
	execPath := "/x/agent-manager-delegated-test"

	msg := delegatedResult(manager, execPath, nil)
	if msg.Err != nil {
		t.Fatalf("success reported %v", msg.Err)
	}
	if msg.Path != execPath {
		t.Fatalf("restart path = %q, want %q", msg.Path, execPath)
	}

	failed := delegatedResult(manager, execPath, errors.New("exit status 1"))
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "brew upgrade --cask yoanwai/tap/agent-manager") {
		t.Fatalf("failure should name the command, got %v", failed.Err)
	}
	if failed.Path != "" {
		t.Fatal("a failed upgrade must not restart")
	}
}
