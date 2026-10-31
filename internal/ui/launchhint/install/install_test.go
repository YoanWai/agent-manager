package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeRetry struct{}

func (fakeRetry) EffectName() string { return "retry" }

func (r fakeRetry) EffectCopy() uieffects.Request { return r }

// fakeRoot is the manager as the runtime reaches it, recording what it asked
// for instead of doing it.
type fakeRoot struct {
	errs      []string
	dones     []string
	launched  []store.Session
	openGen   uint64
	hintOpen  bool
	shown     []string
	retries   []Retry
	refreshes int
}

func (r *fakeRoot) ReportErr(text string) { r.errs = append(r.errs, text) }

func (r *fakeRoot) ReportDone(text string) { r.dones = append(r.dones, text) }

func (r *fakeRoot) SessionLaunched(sess store.Session) { r.launched = append(r.launched, sess) }

func (r *fakeRoot) HintOpen(gen uint64) bool { return r.hintOpen && gen == r.openGen }

func (r *fakeRoot) ShowInstall(sessionID string) tea.Cmd {
	r.shown = append(r.shown, sessionID)
	return nil
}

func (r *fakeRoot) Refresh() tea.Cmd {
	r.refreshes++
	return nil
}

func (r *fakeRoot) QueueRetry(retry Retry) { r.retries = append(r.retries, retry) }

func tempImage(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "prompt.png")
	if err := os.WriteFile(path, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func onPath(t *testing.T, binary string) {
	t.Helper()
	withSeam(t, &CheckInstalled, func(name string) error {
		if name != binary {
			t.Fatalf("checked %q, want %q", name, binary)
		}
		return nil
	})
}

func TestCopyReportsTheOutcome(t *testing.T) {
	original := CopyCommand
	t.Cleanup(func() { CopyCommand = original })

	state := State{Launch: Fix{Command: "install it"}}
	var copied string
	CopyCommand = func(text string) error {
		copied = text
		return nil
	}
	root := &fakeRoot{}

	cmd, ok := state.Route(root, state.Copy()())
	if !ok || cmd != nil {
		t.Fatalf("Route = %v, %t; want a handled copy", cmd, ok)
	}
	if copied != "install it" || len(root.dones) != 1 || root.dones[0] != "copied to clipboard" {
		t.Fatalf("copied %q, done %v", copied, root.dones)
	}

	CopyCommand = func(string) error { return errors.New("no clipboard backend") }
	state.Route(root, state.Copy()())
	if len(root.errs) != 1 || root.errs[0] != "copy failed: no clipboard backend" {
		t.Fatalf("errors = %v, want the copy failure named", root.errs)
	}
}

func TestStartedHandsTheScreenToTheInstaller(t *testing.T) {
	image := tempImage(t)
	state := State{Launch: Fix{Images: []composer.Attachment{{ID: 1, Path: image}}}}
	root := &fakeRoot{hintOpen: true, openGen: 7}
	result := StartResult{
		Session: store.Session{ID: "installer", Name: "install pi"},
		Pending: &Pending{SessionID: "installer", Binary: "pi"},
	}

	state.Started(root, StartRequest{Gen: 7, Binary: "pi"}, result, nil)

	if len(root.launched) != 1 || root.launched[0].ID != "installer" {
		t.Fatalf("launched = %v, want the installer row recorded", root.launched)
	}
	if len(root.shown) != 1 || root.shown[0] != "installer" {
		t.Fatalf("shown = %v, want the installer shell given the screen", root.shown)
	}
	if state.Pending == nil || state.Pending.SessionID != "installer" {
		t.Fatalf("pending = %+v, want the started install tracked", state.Pending)
	}
	if root.dones[0] != "installing pi" {
		t.Fatalf("done = %q, want the install announced", root.dones[0])
	}
}

func TestStartedBehindAClosedCardKeepsTheTracker(t *testing.T) {
	root := &fakeRoot{}
	state := State{}
	result := StartResult{
		Session: store.Session{ID: "installer"},
		Pending: &Pending{SessionID: "installer"},
	}

	state.Started(root, StartRequest{Gen: 1}, result, nil)

	if state.Pending == nil {
		t.Fatal("closed card dropped the running install")
	}
	if len(root.shown) != 0 {
		t.Fatalf("shown = %v, want no screen change behind a closed card", root.shown)
	}
}

// A refused start gives its images back to a card that is still up, and
// drops them when the card is gone.
func TestStartedRetiresImagesItNeverOwned(t *testing.T) {
	for _, open := range []bool{true, false} {
		image := tempImage(t)
		state := State{}
		root := &fakeRoot{hintOpen: open, openGen: 3}
		request := StartRequest{Gen: 3, Images: []composer.Attachment{{ID: 1, Path: image}}}

		cmd := state.Started(root, request, StartResult{}, errors.New("fixture start failed"))
		if open {
			if len(state.Launch.Images) != 1 {
				t.Fatalf("open: fix images = %v, want them handed back", state.Launch.Images)
			}
			if cmd != nil {
				t.Fatal("open: cleanup command returned for images the fix kept")
			}
			continue
		}
		if len(state.Launch.Images) != 0 {
			t.Fatalf("closed: fix kept images %v", state.Launch.Images)
		}
		if cmd == nil {
			t.Fatal("closed: no cleanup command for images nothing holds")
		}
		cmd()
		if _, err := os.Stat(image); !os.IsNotExist(err) {
			t.Fatalf("closed: image survived the cleanup, stat err = %v", err)
		}
	}
}

func TestSettledQueuesTheRetryOnceTheBinaryLands(t *testing.T) {
	retry := fakeRetry{}
	state := State{Pending: &Pending{SessionID: "installer", Binary: "pi", Name: "install-pi"}}
	root := &fakeRoot{}
	request := SettleRequest{Pending: *state.Pending}

	state.Settled(root, request, SettleResult{SessionID: "installer", State: Running}, errors.New("read failed"))
	if len(root.errs) != 1 || state.Pending == nil {
		t.Fatalf("running: errors %v pending %+v, want the failure reported and the install kept", root.errs, state.Pending)
	}

	state.Settled(root, request, SettleResult{SessionID: "installer", State: Running}, nil)
	if state.Pending == nil {
		t.Fatal("a running install was dropped")
	}

	request.Pending.Retry = retry
	state.Settled(root, request, SettleResult{SessionID: "installer", State: Ready}, nil)
	if state.Pending != nil {
		t.Fatalf("ready: pending = %+v, want it cleared", state.Pending)
	}
	if len(root.retries) != 1 || root.retries[0] != retry {
		t.Fatalf("retries = %v, want the captured launch queued", root.retries)
	}
	if root.dones[0] != "pi installed; retry queued" {
		t.Fatalf("done = %q, want the queued retry named", root.dones[0])
	}
}

func TestSettledIgnoresAnInstallItNoLongerTracks(t *testing.T) {
	state := State{Pending: &Pending{SessionID: "newer"}}
	root := &fakeRoot{}

	state.Settled(root, SettleRequest{Pending: Pending{SessionID: "older"}}, SettleResult{SessionID: "older", State: Ready}, nil)

	if state.Pending == nil || state.Pending.SessionID != "newer" || len(root.dones)+len(root.errs) != 0 {
		t.Fatalf("pending = %+v, reports = %v %v; want the newer install untouched", state.Pending, root.dones, root.errs)
	}
}
