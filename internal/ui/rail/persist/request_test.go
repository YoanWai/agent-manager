package persist

import (
	"testing"

	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

// A queued Rail change runs against the store and labels its services hand
// it, and the copy it runs owns the slices the caller still holds.
func TestRequestRunsAgainstItsOwnServices(t *testing.T) {
	st := openStore(t)
	labels := &fakeLabels{live: map[string]bool{}, labels: map[string]string{}}
	folds := []string{"backend"}
	request := Request{Mutation: uirail.Mutation{Kind: uirail.SaveCollapsed, Collapsed: folds}}
	copied := request.EffectCopy().(Request)

	result, err := copied.Run(Services{Store: st, Labels: labels})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, ok := result.(Result); !ok {
		t.Fatalf("run returned %T", result)
	}
	if got := Collapsed(st); len(got) != 1 || got[0] != "backend" {
		t.Fatalf("saved folds = %v", got)
	}
}
