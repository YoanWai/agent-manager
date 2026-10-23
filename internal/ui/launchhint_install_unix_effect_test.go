//go:build unix

package ui

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A ctrl+c in the install tab signals the whole process group, which kills
// the script before its last line: the trap is what still tells the
// manager the install ended, so a pending install cannot outlive it.
func TestInstallScriptReportsAnInterrupt(t *testing.T) {
	dir := t.TempDir()
	statusFile := filepath.Join(dir, "status")
	script := filepath.Join(dir, "install.sh")
	if err := os.WriteFile(script, []byte(installScript("sleep 3", statusFile)), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", script)
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill() })

	// The script echoes the command it is about to run, which the shell
	// reaches only once the trap is armed.
	if _, err := bufio.NewReader(out).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	deadline := time.Now().Add(5 * time.Second)
	for {
		status, err := os.ReadFile(statusFile)
		if err == nil {
			if string(status) != "130" {
				t.Fatalf("status = %q, want the interrupt reported as 130", status)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("an interrupted install never wrote its status")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
