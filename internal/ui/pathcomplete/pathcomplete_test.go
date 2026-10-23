package pathcomplete

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fakeHost reports the path field a read was started from.
type fakeHost struct {
	value string
	open  bool
}

func (h fakeHost) PathField(Target) (string, bool) { return h.value, h.open }

type fixedReader []string

func (r fixedReader) Complete(string) []string { return r }

type blockedReader struct {
	started     chan struct{}
	release     chan struct{}
	once        sync.Once
	suggestions []string
}

func (r *blockedReader) Complete(string) []string {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return r.suggestions
}

func setupDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"alpha", "amber", "beta", ".hidden"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "afile"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// The completer takes a read's answer only for the latest request, and only
// while its field still holds what was typed.
func TestStateTakesOnlyTheLatestReadForUnchangedText(t *testing.T) {
	var pc State
	reader := fixedReader{"/work/alpha", "/work/amber"}
	stale := pc.Request(Group, "/work/a", reader)().(Msg)
	fresh := pc.Request(Group, "/work/a", reader)().(Msg)
	pc.Handle(fakeHost{value: "/work/a", open: true}, stale)
	if pc.Active() {
		t.Fatal("a superseded read filled the list")
	}
	pc.Handle(fakeHost{value: "/work/a", open: false}, fresh)
	if pc.Active() {
		t.Fatal("a read for a closed field filled the list")
	}
	pc.Handle(fakeHost{value: "/work/al", open: true}, fresh)
	if pc.Active() {
		t.Fatal("a read for text since edited filled the list")
	}
	pc.Handle(fakeHost{value: "/work/a", open: true}, fresh)
	if !pc.Move(1) || pc.Selected() != "/work/alpha" {
		t.Fatalf("suggestions = %v", pc.Suggestions)
	}
}

func TestRequestDefersTheScanOffTheUpdatePath(t *testing.T) {
	t.Parallel()
	reader := &blockedReader{started: make(chan struct{}), release: make(chan struct{}), suggestions: []string{"/first-result"}}
	var pc State
	cmd := pc.Request(Form, "/first", reader)
	select {
	case <-reader.started:
		t.Fatal("path scan ran on the update path")
	default:
	}
	completed := make(chan Msg, 1)
	go func() { completed <- cmd().(Msg) }()
	<-reader.started
	pc.Request(Form, "/newer", SystemReader{})
	close(reader.release)
	pc.Handle(fakeHost{value: "/first", open: true}, <-completed)
	if len(pc.Suggestions) != 0 {
		t.Fatalf("stale scan replaced newer input: %v", pc.Suggestions)
	}
}

func TestCompleteDirsMatchesPrefix(t *testing.T) {
	root := setupDir(t)
	got := completeDirs(filepath.Join(root, "a"))
	want := []string{filepath.Join(root, "alpha"), filepath.Join(root, "amber")}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestCompleteDirsTrailingSlashListsChildren(t *testing.T) {
	root := setupDir(t)
	got := completeDirs(root + "/")
	if len(got) != 3 {
		t.Fatalf("expected 3 visible dirs, got %v", got)
	}
	for _, path := range got {
		if filepath.Base(path) == ".hidden" || filepath.Base(path) == "afile" {
			t.Fatalf("unexpected entry %s", path)
		}
	}
}

func TestCompleteDirsHiddenNeedsDotPrefix(t *testing.T) {
	root := setupDir(t)
	got := completeDirs(filepath.Join(root, ".h"))
	if len(got) != 1 || filepath.Base(got[0]) != ".hidden" {
		t.Fatalf("got %v", got)
	}
}

func TestCompleteDirsNoSlashNoSuggestions(t *testing.T) {
	if got := completeDirs("relative"); got != nil {
		t.Fatalf("got %v", got)
	}
	if got := completeDirs(""); got != nil {
		t.Fatalf("got %v", got)
	}
}

func TestMoveAndPickKeepTheHighlightInsideTheList(t *testing.T) {
	pc := State{Suggestions: []string{"/a", "/b"}}
	if pc.Move(-1) {
		t.Fatal("up off an unchosen list should leave the field")
	}
	if !pc.Move(1) || pc.Index != 0 || !pc.Chosen {
		t.Fatalf("first down = index %d chosen %t", pc.Index, pc.Chosen)
	}
	if !pc.Move(1) || pc.Index != 1 {
		t.Fatalf("second down = index %d", pc.Index)
	}
	if pc.Move(1) {
		t.Fatal("down past the last suggestion should leave the field")
	}
	if pc.Pick(2) {
		t.Fatal("a pick past the list should be refused")
	}
	if !pc.Pick(0) || pc.Selected() != "/a" {
		t.Fatalf("pick 0 = %q", pc.Selected())
	}
	pc.Reset()
	if pc.Active() || pc.Selected() != "" {
		t.Fatalf("reset left %v", pc.Suggestions)
	}
}
