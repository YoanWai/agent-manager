package tmux

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func requireTmux(t *testing.T) *Driver {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not installed")
	}
	driver, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return driver
}

func TestSetLabelNeutralizesFormatStrings(t *testing.T) {
	driver := requireTmux(t)
	id := "lbl" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	marker := "/tmp/am-injection-" + id
	if err := driver.SetLabel(id, "evil #(touch "+marker+") name"); err != nil {
		t.Fatalf("SetLabel: %v", err)
	}
	rendered, err := exec.Command("tmux", "display-message", "-p", "-t", "am_"+id, "#{T:status-left}").CombinedOutput()
	if err != nil {
		t.Fatalf("display-message: %v", err)
	}
	if !strings.Contains(string(rendered), "#(touch") {
		t.Fatalf("format string should render literally, got %q", rendered)
	}
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		os.Remove(marker)
		t.Fatal("injection executed: marker file was created")
	}
}

func TestLifecycle(t *testing.T) {
	driver := requireTmux(t)
	id := "test" + time.Now().Format("150405.000000")
	id = strings.ReplaceAll(id, ".", "")

	if err := driver.Create(id, "/tmp", "printf 'hello-pane-marker'", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if !driver.Exists(id) {
		t.Fatal("session should exist after Create")
	}

	pid, err := driver.PanePID(id)
	if err != nil || pid <= 0 {
		t.Fatalf("PanePID: pid=%d err=%v", pid, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var pane string
	for time.Now().Before(deadline) {
		pane, err = driver.CapturePane(id)
		if err != nil {
			t.Fatalf("CapturePane: %v", err)
		}
		if strings.Contains(pane, "hello-pane-marker") {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(pane, "hello-pane-marker") {
		t.Fatalf("captured pane missing marker: %q", pane)
	}

	ids, err := driver.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, got := range ids {
		if got == id {
			found = true
		}
	}
	if !found {
		t.Fatalf("List should include %q, got %v", id, ids)
	}

	if err := driver.Kill(id); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	if driver.Exists(id) {
		t.Fatal("session should be gone after Kill")
	}
	if err := driver.Kill(id); err != nil {
		t.Fatalf("Kill on missing session should be a no-op, got %v", err)
	}
}
