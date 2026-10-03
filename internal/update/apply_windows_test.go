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
// handle forbids the rename too and would not model the update.
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
	defer func() {
		stdin.Close()
		running.Wait()
	}()

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
	aside, err := os.ReadFile(target + ".old")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(aside, oldBinary) {
		t.Fatal("old binary should sit at target.old")
	}
}
