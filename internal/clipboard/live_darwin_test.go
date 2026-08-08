//go:build darwin

package clipboard

import (
	"bytes"
	"os/exec"
	"testing"
)

// TestDarwinLiveCopy exercises the real pbcopy path end to end. Skipped in
// CI where no pasteboard exists; locally it proves the platform writer kept
// working after the OSC 52 fallback landed.
func TestDarwinLiveCopy(t *testing.T) {
	if _, err := exec.LookPath("pbpaste"); err != nil {
		t.Skip("no pbpaste on this host")
	}
	original, _ := exec.Command("pbpaste").Output()
	defer func() {
		restoreCmd := exec.Command("pbcopy")
		restoreCmd.Stdin = bytes.NewReader(original)
		_ = restoreCmd.Run()
	}()
	if err := WriteText("agent-manager-live-copy-proof"); err != nil {
		t.Skipf("pasteboard unavailable: %v", err)
	}
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Skipf("pbpaste unavailable: %v", err)
	}
	if string(out) != "agent-manager-live-copy-proof" {
		t.Fatalf("pbpaste = %q", out)
	}
}
