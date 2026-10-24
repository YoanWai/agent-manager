package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
)

func withSeam[T any](t *testing.T, seam *T, value T) {
	t.Helper()
	original := *seam
	*seam = value
	t.Cleanup(func() { *seam = original })
}

// RunSettle reads the status the installer shell wrote, and retires the
// files and images of an install that ended.
func TestRunSettleReadsTheStatusAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "prompt.png")
	if err := os.WriteFile(image, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	onPath(t, "am-fake-cli")

	result, err := RunSettle(Services{}, SettleRequest{Pending: Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
		Script:     script,
		Images:     []composer.Attachment{{ID: 1, Path: image}},
	}})

	if err != nil {
		t.Fatal(err)
	}
	if result.State != Ready {
		t.Fatalf("state = %v, want the binary reported ready", result.State)
	}
	for _, path := range []string{statusFile, script, image} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s survived a finished install, stat err = %v", path, err)
		}
	}
}

// A status read that fails for any other reason leaves the install tracked so
// the next poll observes it again.
func TestRunSettleKeepsAnUnreadableStatus(t *testing.T) {
	dir := t.TempDir()
	statusDir := filepath.Join(dir, "status-dir")
	if err := os.Mkdir(statusDir, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := RunSettle(Services{}, SettleRequest{Pending: Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusDir,
	}})

	if err == nil || !strings.Contains(err.Error(), "install-am-fake-cli") {
		t.Fatalf("err = %v, want the installer named", err)
	}
}

// A shell that exited without a binary on PATH reports the missing binary
// rather than a ready install.
func TestRunSettleReportsAMissingBinary(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "install.status")
	if err := os.WriteFile(statusFile, []byte("0"), 0o600); err != nil {
		t.Fatal(err)
	}
	withSeam(t, &CheckInstalled, func(binary string) error {
		return config.MissingToolError{Binary: binary}
	})

	result, err := RunSettle(Services{}, SettleRequest{Pending: Pending{
		SessionID:  "installer",
		Name:       "install-am-fake-cli",
		Binary:     "am-fake-cli",
		StatusFile: statusFile,
	}})

	if err != nil {
		t.Fatal(err)
	}
	if result.State != BinaryMissing {
		t.Fatalf("state = %v, want the binary reported missing", result.State)
	}
}

// RunStart refuses before it writes anything when the services are missing.
func TestRunStartRefusesWithoutServices(t *testing.T) {
	_, err := RunStart(Services{}, StartRequest{Binary: "pi"})
	if err == nil || !strings.Contains(err.Error(), "lifecycle") {
		t.Fatalf("err = %v, want the missing services named", err)
	}
}
