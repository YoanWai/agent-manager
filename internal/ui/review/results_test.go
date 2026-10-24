package review

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type carryRecorder struct {
	forgot  []string
	refresh int
	errs    []string
	asks    Requests
}

func (r *carryRecorder) ForgetPreferredRepo(repo string) { r.forgot = append(r.forgot, repo) }
func (r *carryRecorder) ReportErr(text string)           { r.errs = append(r.errs, text) }
func (r *carryRecorder) RequestRefresh()                 { r.refresh++ }

func (r *carryRecorder) ReviewCommands(reqs Requests) tea.Cmd {
	r.asks = reqs
	return func() tea.Msg { return reqs }
}

// A decision nobody accepted carries nothing: no complaint reaches the root and
// no work goes back through the command builder.
func TestCarryDropsAnUnacceptedDecision(t *testing.T) {
	root := &carryRecorder{}
	if cmd := Carry(root, ApplyResult{Error: "stale load", Refresh: true}); cmd != nil {
		t.Fatal("an unaccepted decision produced work")
	}
	if len(root.errs) != 0 || root.refresh != 0 {
		t.Fatalf("an unaccepted decision reached the root: %+v", root)
	}
}

// An accepted decision hands its error and its forgotten repo to the root, and
// its requests to the command builder.
func TestCarryReportsAndForwardsAnAcceptedDecision(t *testing.T) {
	root := &carryRecorder{}
	cmd := Carry(root, ApplyResult{
		Accepted:            true,
		Error:               "picked repo is gone",
		ForgetPreferredRepo: "/repo/gone",
		Refresh:             true,
		Requests:            Requests{StartupTick: true},
	})
	if cmd == nil {
		t.Fatal("an accepted decision produced no work")
	}
	if len(root.errs) != 1 || root.errs[0] != "picked repo is gone" {
		t.Fatalf("errors = %v", root.errs)
	}
	if len(root.forgot) != 1 || root.forgot[0] != "/repo/gone" {
		t.Fatalf("forgot = %v", root.forgot)
	}
	if root.refresh != 1 {
		t.Fatalf("refresh = %d", root.refresh)
	}
	if !root.asks.StartupTick {
		t.Fatalf("requests = %+v", root.asks)
	}
}
