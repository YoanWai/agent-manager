package ui

import "testing"

// Every poll enqueues the review status read while a review is open, and
// the install settle check while an installer runs. A poll asked for after
// either would run the next pass at once, so one open review spun the
// poller in a tight loop (#880). Effects a user action starts still ask
// for the poll that shows their result.
func TestPollAfterEffectSkipsThePerPollReads(t *testing.T) {
	cases := []struct {
		name    string
		request effectRequest
		want    bool
	}{
		{"review status", reviewEffectRequest{op: reviewOpStatus}, false},
		{"install settle", installSettleRequest{}, false},
		{"review save", reviewEffectRequest{op: reviewOpSave}, true},
		{"review send", reviewEffectRequest{op: reviewOpSend}, true},
		{"pane resize", geometryRequest{}, true},
		{"session lifecycle", lifecycleRequest{}, true},
	}
	for _, tc := range cases {
		if got := pollAfterEffect(tc.request); got != tc.want {
			t.Errorf("%s: pollAfterEffect = %v, want %v", tc.name, got, tc.want)
		}
	}
}
