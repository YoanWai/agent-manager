package mcpreg

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
)

func TestApplyPreservesHermesSDKCheckTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake Hermes executable is a shell script")
	}
	for _, step := range []string{"version", "pip"} {
		t.Run(step, func(t *testing.T) {
			python := fakeHermesEnvironment(t, 0)
			version := "printf 'Install directory: %s\\n' " + tmux.ShellQuote(filepath.Join(filepath.Dir(filepath.Dir(python)), "lib", "python3.14", "site-packages"))
			if step == "version" {
				version = "exec sleep 30"
			} else if err := os.WriteFile(python, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			dir := fakeCLI(t, "hermes", "case \"$1\" in\nconfig) exit 1 ;;\n--version) "+version+" ;;\n*) printf \"requires the 'mcp' Python SDK\\n\" ;;\nesac\n")
			shortenRegistrationTimeout(t, 300*time.Millisecond)
			_, err := Apply("hermes", "/opt/bin/agent-manager", dir, "hermes", map[string]string{})
			var timeout *TimeoutError
			if !errors.As(err, &timeout) || timeout.Tool != "hermes" {
				t.Fatalf("err = %v, want a Hermes TimeoutError", err)
			}
			var unavailable HermesMCPUnavailableError
			if errors.As(err, &unavailable) {
				t.Fatalf("timeout became an unavailable-SDK error: %v", err)
			}
			assertNoMarker(t, dir, "hermes")
		})
	}
}
