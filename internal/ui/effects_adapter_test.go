package ui

import (
	"testing"

	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uigeometrypanes "github.com/YoanWai/agent-manager/internal/ui/geometry/panes"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
)

// Every poll enqueues the review status read while a review is open, and
// the install settle check while an installer runs. A poll asked for after
// either would run the next pass at once, so one open review spun the
// poller in a tight loop (#880). Effects a user action starts still ask
// for the poll that shows their result.
func TestPollAfterEffectSkipsThePerPollReads(t *testing.T) {
	change := func(op uireviewsource.Op) uireviewsource.Change {
		return uireviewsource.Change{Request: uireviewsource.Request{Op: op}}
	}
	cases := []struct {
		name    string
		request uieffects.Request
		want    bool
	}{
		{"review status", change(uireviewsource.Status), false},
		{"install settle", uiinstall.SettleRequest{}, false},
		{"review save", change(uireviewsource.Save), true},
		{"review send", change(uireviewsource.Send), true},
		{"pane resize", uigeometrypanes.ResizeRequest{}, true},
		{"session lifecycle", uilifecycle.Request{}, true},
	}
	for _, tc := range cases {
		if got := pollAfterEffect(tc.request); got != tc.want {
			t.Errorf("%s: pollAfterEffect = %v, want %v", tc.name, got, tc.want)
		}
	}
}
