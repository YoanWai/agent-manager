package lifecycle

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestEffectNameSaysWhichActionRuns(t *testing.T) {
	t.Parallel()
	for action, want := range map[Action]string{Remove: "session delete", Archive: "session archive", Restart: "session restart"} {
		if got := (Request{Target: Target{Action: action}}).EffectName(); got != want {
			t.Errorf("EffectName(%q) = %q, want %q", action, got, want)
		}
	}
}

// A queued action owns the rows it was confirmed with: editing the loaded
// list afterwards must not retarget work that is already on the lane.
func TestRequestCopyOwnsNestedSessionValues(t *testing.T) {
	sessions := []store.Session{{ID: "captured", PendingInputs: []string{"first"}, RelaunchSnapshot: map[string]int64{"conversation": 1}}}
	copied := Request{Target: Target{Sessions: sessions}}.EffectCopy().(Request)
	sessions[0].PendingInputs[0] = "changed"
	sessions[0].RelaunchSnapshot["conversation"] = 2
	if copied.Target.Sessions[0].PendingInputs[0] != "first" || copied.Target.Sessions[0].RelaunchSnapshot["conversation"] != 1 {
		t.Fatal("request retained mutable session aliases")
	}
}

func TestEveryRequestNamesItself(t *testing.T) {
	t.Parallel()
	for _, request := range []interface{ EffectName() string }{
		Request{},
		AfterTurnRequest{},
		AttachRequest{},
	} {
		if request.EffectName() == "" {
			t.Errorf("%T names itself with nothing", request)
		}
	}
}
