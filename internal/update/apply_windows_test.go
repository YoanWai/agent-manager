//go:build windows

package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestHoldRunningHelper is the process TestApplySwapsARunningBinaryAside
// runs from the target path; it lives until its stdin closes.
func TestHoldRunningHelper(t *testing.T) {
	if os.Getenv("AGENT_MANAGER_HOLD_RUNNING") != "1" {
		return
	}
	io.Copy(io.Discard, os.Stdin)
}

// A running executable is mapped with rename sharing but not delete or
// overwrite, so the target has to be a live process image: a plain os.Open
// handle forbids the rename too and would not model the update. The first
// manager outlives a restart as the new one's parent, so a second update
// runs while the first binary moved aside is still locked.
func TestApplySwapsARunningBinaryAside(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	oldBinary, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	newBinary := []byte("new build bytes")
	archive := releaseArchive(t, newBinary)
	digest := sha256.Sum256(archive)
	serveRelease(t, "v9.9.9", archive, hex.EncodeToString(digest[:]))

	target := filepath.Join(t.TempDir(), binaryName)
	if err := os.WriteFile(target, oldBinary, 0o755); err != nil {
		t.Fatal(err)
	}
	running := exec.Command(target, "-test.run=^TestHoldRunningHelper$")
	running.Env = append(os.Environ(), "AGENT_MANAGER_HOLD_RUNNING=1")
	stdin, err := running.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := running.Start(); err != nil {
		t.Fatal(err)
	}
	stopRunning := func() {
		stdin.Close()
		running.Wait()
	}
	defer stopRunning()

	if err := Apply(context.Background(), "v9.9.9", target); err != nil {
		t.Fatalf("apply over a running binary: %v", err)
	}
	swapped, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(swapped, newBinary) {
		t.Fatalf("binary not swapped, got %d bytes", len(swapped))
	}
	aside := asideBinaries(t, target)
	if len(aside) != 1 {
		t.Fatalf("binaries moved aside = %v, want one", aside)
	}
	held, err := os.ReadFile(aside[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(held, oldBinary) {
		t.Fatal("the running binary should sit beside the target")
	}

	if err := Apply(context.Background(), "v9.9.9", target); err != nil {
		t.Fatalf("a second update while the first old binary still runs: %v", err)
	}
	if got := asideBinaries(t, target); len(got) != 2 || got[0] != aside[0] {
		t.Fatalf("binaries moved aside = %v, want the running one kept and one more", got)
	}

	stopRunning()
	unrelated := target + ".old"
	if err := os.WriteFile(unrelated, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), "v9.9.9", target); err != nil {
		t.Fatalf("a third update: %v", err)
	}
	if got := asideBinaries(t, target); len(got) != 1 || got[0] == aside[0] {
		t.Fatalf("binaries moved aside = %v, want only the latest once nothing runs the older ones", got)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("a file the update did not create was removed: %v", err)
	}
}

func asideBinaries(t *testing.T, target string) []string {
	t.Helper()
	matches, err := filepath.Glob(target + ".old-*")
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
