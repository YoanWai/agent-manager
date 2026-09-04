package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestFocusRequestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := TakeFocus(dir); ok {
		t.Fatal("nothing should be pending in an empty directory")
	}
	if err := RequestFocus(dir, os.Getpid(), "sess-3"); err != nil {
		t.Fatal(err)
	}
	id, ok := TakeFocus(dir)
	if !ok || id != "sess-3" {
		t.Fatalf("TakeFocus = %q, %v; want sess-3", id, ok)
	}
	if _, err := os.Stat(focusPath(dir, os.Getpid())); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a taken request must not be served twice")
	}
}

// Two managers polling the same directory must never both act on one
// click. A click published while another is still pending replaces it,
// which is the intent: the newest click is the one the user meant.
func TestTakeFocusClaimsEachRequestOnce(t *testing.T) {
	dir := t.TempDir()
	if err := RequestFocus(dir, os.Getpid(), "sess-first"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	taken := map[string]int{}
	var writes, reads sync.WaitGroup
	// Readers that stopped on a fixed count could all finish before the
	// first write landed, leaving the test green with nothing claimed.
	published := make(chan struct{})
	for writer := 0; writer < 3; writer++ {
		writes.Add(1)
		go func(writer int) {
			defer writes.Done()
			for i := 0; i < 50; i++ {
				if err := RequestFocus(dir, os.Getpid(), "sess-"+strconv.Itoa(writer)+"-"+strconv.Itoa(i)); err != nil {
					t.Error(err)
					return
				}
			}
		}(writer)
	}
	for reader := 0; reader < 4; reader++ {
		reads.Add(1)
		go func() {
			defer reads.Done()
			for {
				if id, ok := TakeFocus(dir); ok {
					mu.Lock()
					taken[id]++
					mu.Unlock()
					continue
				}
				select {
				case <-published:
					// One last look, so the final write cannot be missed
					// by a reader that gave up between it and the close.
					if id, ok := TakeFocus(dir); ok {
						mu.Lock()
						taken[id]++
						mu.Unlock()
					}
					return
				default:
				}
			}
		}()
	}
	writes.Wait()
	close(published)
	reads.Wait()
	if len(taken) == 0 {
		t.Fatal("no click was ever claimed")
	}
	for id, count := range taken {
		if id == "" {
			t.Fatal("a claim returned an empty session")
		}
		if count != 1 {
			t.Fatalf("%s was claimed %d times, want once", id, count)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(focusPath(dir, os.Getpid())) {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}

// Two managers share the config directory, and a click belongs to the one
// whose banner was clicked.
func TestTakeFocusLeavesAnotherManagersClick(t *testing.T) {
	dir := t.TempDir()
	if err := RequestFocus(dir, os.Getpid()+1, "sess-elsewhere"); err != nil {
		t.Fatal(err)
	}
	if id, ok := TakeFocus(dir); ok {
		t.Fatalf("claimed %q, which belongs to another manager", id)
	}
	if err := RequestFocus(dir, os.Getpid(), "sess-mine"); err != nil {
		t.Fatal(err)
	}
	id, ok := TakeFocus(dir)
	if !ok || id != "sess-mine" {
		t.Fatalf("TakeFocus = %q, %v; want sess-mine", id, ok)
	}
	if _, err := os.Stat(focusPath(dir, os.Getpid()+1)); err != nil {
		t.Fatal("the other manager's click should still be waiting for it")
	}
}

func TestSweepAbandonedFocusKeepsLiveRequests(t *testing.T) {
	dir := t.TempDir()
	abandoned := focusPath(dir, os.Getpid()+1)
	if err := RequestFocus(dir, os.Getpid()+1, "sess-abandoned"); err != nil {
		t.Fatal(err)
	}
	aged := time.Now().Add(-clickTimeout - time.Minute)
	if err := os.Chtimes(abandoned, aged, aged); err != nil {
		t.Fatal(err)
	}
	if err := RequestFocus(dir, os.Getpid(), "sess-fresh"); err != nil {
		t.Fatal(err)
	}
	sweepAbandonedFocus(dir)
	if _, err := os.Stat(abandoned); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a request older than the click window should be gone")
	}
	if id, ok := TakeFocus(dir); !ok || id != "sess-fresh" {
		t.Fatalf("TakeFocus = %q, %v; want the fresh request", id, ok)
	}
}
