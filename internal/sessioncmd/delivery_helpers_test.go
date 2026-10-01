package sessioncmd

import (
	"context"
	"fmt"
	"github.com/YoanWai/agent-manager/internal/store"
	"testing"
	"time"
)

func finishMessageFixture(st *store.Store, outcome store.DeliveryOutcome, id int64, at time.Time) error {
	acquired, err := st.WithDeliveryGuard(context.Background(), func(g *store.DeliveryGuard) error {
		claim, claimed, err := g.ClaimMessage(id, at)
		if err != nil {
			return err
		}
		if !claimed {
			return fmt.Errorf("fixture message %d was not claimed", id)
		}
		return g.FinishMessage(id, claim, outcome, at)
	})
	if err != nil {
		return err
	}
	if !acquired {
		return fmt.Errorf("fixture delivery guard busy")
	}
	return nil
}

func TestSessionInfoExposesPendingInputUncertainty(t *testing.T) {
	r := &runtime{}
	got := r.sessionInfo(store.Session{ID: "target", PendingInputOutcome: store.DeliveryUncertain}, false, false)
	if got.PendingInputOutcome != "uncertain" {
		t.Fatalf("queued-input receipt hidden: %+v", got)
	}
}
