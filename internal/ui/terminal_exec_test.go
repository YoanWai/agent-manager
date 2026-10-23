package ui

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestExecTerminalProcessKeepsChildStdoutOnTheTerminal(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "true")
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
	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("close temporary stdout file: %v", err)
		}
	}()

	cmd := exec.CommandContext(context.Background(), "sh", "-c", "true")
	cmd.Stdout = file
	execTerminalProcess(cmd, nil)
	if cmd.Stdout != file {
		t.Fatal("preconfigured child stdout was overwritten")
	}
}
