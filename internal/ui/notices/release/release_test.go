package release

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/update"
)

func TestDelegatedResult(t *testing.T) {
	manager := update.Manager{Name: "Homebrew", Command: []string{"brew", "upgrade", "--cask", "yoanwai/tap/agent-manager"}}
	// A base name no PATH holds, so RestartTarget deterministically falls
	// back to execPath on any machine.
	execPath := "/x/agent-manager-delegated-test"

	msg := delegatedResult(manager, execPath, nil)
	if msg.Err != nil {
		t.Fatalf("success reported %v", msg.Err)
	}
	if msg.Path != execPath {
		t.Fatalf("restart path = %q, want %q", msg.Path, execPath)
	}

	failed := delegatedResult(manager, execPath, errors.New("exit status 1"))
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "brew upgrade --cask yoanwai/tap/agent-manager") {
		t.Fatalf("failure should name the command, got %v", failed.Err)
	}
	if failed.Path != "" {
		t.Fatal("a failed upgrade must not restart")
	}
}

func TestSourcesMapsFeedKindsActionBannerAndExpiry(t *testing.T) {
	expires := time.Date(2026, time.November, 20, 0, 0, 0, 0, time.UTC)
	state := State{Feed: []feed.Message{
		{ID: "a", Kind: feed.KindHeadsUp, Banner: "Soon", Action: "Read it", ExpiresAt: expires},
		{ID: "b", Kind: feed.KindTip},
		{ID: "c", Kind: feed.KindNews},
		{ID: "d", Kind: "mystery"},
	}}
	got := state.Sources().Feed
	wantKinds := []notices.Kind{notices.KindHeadsUp, notices.KindTip, notices.KindNews, notices.KindNews}
	for i, want := range wantKinds {
		if got[i].Kind != want {
			t.Fatalf("feed[%d].Kind = %v, want %v", i, got[i].Kind, want)
		}
	}
	if got[0].Banner != "Soon" || got[0].Action != "Read it" || !got[0].Expires.Equal(expires) {
		t.Fatalf("feed[0] = %+v", got[0])
	}
}
