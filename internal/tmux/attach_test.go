package tmux

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Resize pins the detached window to a manual size for the preview, and
// PrepareAttach flips it back to auto so the attaching client fills it
// instead of leaving tmux's dotted out-of-bounds overlay on the right.
func TestPrepareAttachRestoresAutoSize(t *testing.T) {
	driver := requireTmux(t)
	id := "attach" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	if err := driver.Resize(id, 80, 24); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if got := windowSizeOption(t, id); got != "manual" {
		t.Fatalf("after Resize, window-size = %q, want manual", got)
	}

	if err := driver.PrepareAttach(id); err != nil {
		t.Fatalf("PrepareAttach: %v", err)
	}
	want := "latest"
	if driver.attachSizeLargest.Load() {
		want = "largest"
	}
	if got := windowSizeOption(t, id); got != want {
		t.Fatalf("after PrepareAttach, window-size = %q, want %q", got, want)
	}
}

// A pre-3.1 server rejects window-size "latest" with "unknown value";
// PrepareAttach must retry with "largest" and remember the verdict. A stub
// tmux that rejects "latest" stands in for the old server and logs its
// calls, so the test also proves the second attach skips the doomed try.
func TestPrepareAttachFallsBackWhenLatestRejected(t *testing.T) {
	dir := t.TempDir()
	callLog := dir + "/calls"
	stub := dir + "/tmux"
	script := "#!/bin/sh\necho \"$@\" >> " + callLog + "\ncase \"$*\" in *latest*) echo 'unknown value: latest' >&2; exit 1;; esac\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatalf("stub: %v", err)
	}
	driver := &Driver{bin: stub, socket: testSocket}

	if err := driver.PrepareAttach("x1"); err != nil {
		t.Fatalf("PrepareAttach with rejecting server: %v", err)
	}
	if err := driver.PrepareAttach("x1"); err != nil {
		t.Fatalf("second PrepareAttach: %v", err)
	}
	logged, err := os.ReadFile(callLog)
	if err != nil {
		t.Fatalf("read call log: %v", err)
	}
	calls := strings.Split(strings.TrimSpace(string(logged)), "\n")
	if len(calls) != 3 {
		t.Fatalf("got %d tmux calls, want 3 (latest, largest, largest):\n%s", len(calls), logged)
	}
	for i, wantValue := range []string{"latest", "largest", "largest"} {
		if !strings.HasSuffix(calls[i], "window-size "+wantValue) {
			t.Fatalf("call %d = %q, want window-size %s", i, calls[i], wantValue)
		}
	}
}

func TestDetachRequestRoundTrip(t *testing.T) {
	driver := requireTmux(t)
	// A live server is needed for global options to stick.
	id := "rev" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	t.Cleanup(func() {
		if err := driver.ClearRequest(); err != nil {
			t.Errorf("ClearRequest: %v", err)
		}
	})

	if err := driver.ClearRequest(); err != nil {
		t.Fatalf("ClearRequest: %v", err)
	}
	request, err := driver.PendingRequest()
	if err != nil {
		t.Fatalf("PendingRequest: %v", err)
	}
	if request != "" {
		t.Fatalf("no request expected on a clean marker, got %q", request)
	}

	for _, want := range []string{RequestReview, RequestEditor} {
		if _, err := tmuxCmd("set-option", "-g", requestOption, want).CombinedOutput(); err != nil {
			t.Fatalf("set marker: %v", err)
		}
		request, err = driver.PendingRequest()
		if err != nil {
			t.Fatalf("PendingRequest: %v", err)
		}
		if request != want {
			t.Fatalf("marker reads as %q, want %q", request, want)
		}

		if err := driver.ClearRequest(); err != nil {
			t.Fatalf("ClearRequest: %v", err)
		}
		request, err = driver.PendingRequest()
		if err != nil {
			t.Fatalf("PendingRequest: %v", err)
		}
		if request != "" {
			t.Fatalf("clear should drop the marker, got %q", request)
		}
	}
}
