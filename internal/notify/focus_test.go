package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func TestFocusRequestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := TakeFocus(dir); ok {
		t.Fatal("nothing should be pending in an empty directory")
	}
	if err := RequestFocus(dir, "sess-3"); err != nil {
		t.Fatal(err)
	}
	id, ok := TakeFocus(dir)
	if !ok || id != "sess-3" {
		t.Fatalf("TakeFocus = %q, %v; want sess-3", id, ok)
	}
	if _, err := os.Stat(filepath.Join(dir, focusFile)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a taken request must not be served twice")
	}
}

// Two managers polling the same directory must never both act on one
// click. A click published while another is still pending replaces it,
// which is the intent: the newest click is the one the user meant.
func TestTakeFocusClaimsEachRequestOnce(t *testing.T) {
	dir := t.TempDir()
	if err := RequestFocus(dir, "sess-first"); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	taken := map[string]int{}
	var writes, reads sync.WaitGroup
	for writer := 0; writer < 3; writer++ {
		writes.Add(1)
		go func(writer int) {
			defer writes.Done()
			for i := 0; i < 50; i++ {
				if err := RequestFocus(dir, "sess-"+strconv.Itoa(writer)+"-"+strconv.Itoa(i)); err != nil {
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
			for i := 0; i < 200; i++ {
				if id, ok := TakeFocus(dir); ok {
					mu.Lock()
					taken[id]++
					mu.Unlock()
				}
			}
		}()
	}
	writes.Wait()
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
		if entry.Name() != focusFile {
			t.Fatalf("temporary file left behind: %s", entry.Name())
		}
	}
}
