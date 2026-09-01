package ui

import (
	"os"
	"os/exec"
	"testing"
)

func TestExecTerminalProcessKeepsChildStdoutOnTheTerminal(t *testing.T) {
	cmd := exec.Command("sh", "-c", "true")
	execTerminalProcess(cmd, nil)
	if cmd.Stdout != os.Stdout {
		t.Fatal("interactive child stdout was not pinned to the terminal")
	}
}

func TestExecTerminalProcessPreservesConfiguredStdout(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	cmd := exec.Command("sh", "-c", "true")
	cmd.Stdout = file
	execTerminalProcess(cmd, nil)
	if cmd.Stdout != file {
		t.Fatal("preconfigured child stdout was overwritten")
	}
}
