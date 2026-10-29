package ui

import (
	"context"
	"io"
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

type recordedRun struct{ stdout *os.File }

func (r recordedRun) Run() error {
	_, err := r.stdout.WriteString("child")
	return err
}

func (recordedRun) SetStdin(io.Reader)  {}
func (recordedRun) SetStdout(io.Writer) {}
func (recordedRun) SetStderr(io.Writer) {}

// A child owns the terminal with mode 2031 off, so a scheme flip never
// reaches it as typed input, and the manager subscribes again after.
func TestSchemeReportsPausedAroundChild(t *testing.T) {
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	saved := os.Stdout
	os.Stdout = write
	runErr := schemeReportsPaused{recordedRun{stdout: write}}.Run()
	os.Stdout = saved
	if err := write.Close(); err != nil {
		t.Fatalf("close write end: %v", err)
	}
	if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	got, err := io.ReadAll(read)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if want := "\x1b[?2031l" + "child" + "\x1b[?2031h"; string(got) != want {
		t.Fatalf("emitted %q, want %q", got, want)
	}
}
