package terminal

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

func TestExecProcessKeepsChildStdoutOnTheTerminal(t *testing.T) {
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "true")
	ExecProcess(cmd, nil)
	if cmd.Stdout != os.Stdout {
		t.Fatal("interactive child stdout was not pinned to the terminal")
	}
}

func TestExecProcessPreservesConfiguredStdout(t *testing.T) {
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
	ExecProcess(cmd, nil)
	if cmd.Stdout != file {
		t.Fatal("preconfigured child stdout was overwritten")
	}
}
