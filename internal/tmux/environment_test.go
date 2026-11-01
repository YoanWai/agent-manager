//go:build !windows

package tmux

import (
	"strings"
	"testing"
	"time"
)

func TestServerEnvironmentSetsAndClears(t *testing.T) {
	driver := requireTmux(t)
	id := "serverenv" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.SetServerEnvironment("AM_TEST_FRAMES", "shm"); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := tmuxCmd("show-environment", "-g", "AM_TEST_FRAMES").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "AM_TEST_FRAMES=shm" {
		t.Fatalf("after set: %q %v", out, err)
	}

	if err := driver.SetServerEnvironment("AM_TEST_FRAMES", ""); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := driver.SetServerEnvironment("AM_TEST_FRAMES", ""); err != nil {
		t.Fatalf("clearing an unset variable: %v", err)
	}
	out, _ = tmuxCmd("show-environment", "-g", "AM_TEST_FRAMES").CombinedOutput()
	if strings.Contains(string(out), "AM_TEST_FRAMES=") {
		t.Fatalf("after clear: %q", out)
	}
}

func TestServerEnvironmentWithNoServerIsANoOp(t *testing.T) {
	requireTmux(t)
	driver, err := NewWithSocket("amnoserver" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", ""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := driver.SetServerEnvironment("AM_TEST_FRAMES", "shm"); err != nil {
		t.Fatalf("no server: %v", err)
	}
}
