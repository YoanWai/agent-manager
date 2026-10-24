package link

import (
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// The save runs as an effect: it writes the list, reads it back, and hands the
// store's answer to the manager.
func TestSaveRequestRunsAgainstTheStore(t *testing.T) {
	st := openStore(t)
	request := SaveRequest{Op: SaveAdd, Next: store.Connection{Name: "box", Destination: "me@box"}}
	result, err := request.Run(Services{Store: st})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	saved, ok := result.(SaveResult)
	if !ok || len(saved.Connections) != 1 || saved.Connections[0].Name != "box" {
		t.Fatalf("result = %+v", result)
	}
	if request.EffectName() == "" {
		t.Fatal("a save names itself with nothing")
	}
}

// A removal runs the same way and reports the list without the row.
func TestSaveRemoveRunReadsTheListBack(t *testing.T) {
	st := openStore(t)
	if err := st.AddConnection(store.Connection{Name: "box", Destination: "me@box"}); err != nil {
		t.Fatal(err)
	}
	result, err := SaveRequest{Op: SaveRemove, Name: "box"}.Run(Services{Store: st})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if saved := result.(SaveResult); len(saved.Connections) != 0 {
		t.Fatalf("list = %+v after the removal", saved.Connections)
	}
}

var _ uieffects.Request = SaveRequest{}
var _ uieffects.Request = Call{}

// The card's own message is routed by the package: a cancel asks for the
// status line to be cleared and carries no save, a save carries its request.
func TestRouteAnswersTheCardsMessage(t *testing.T) {
	if _, ok := Route("something else"); ok {
		t.Fatal("an unrelated message was routed")
	}
	save := SaveRequest{Op: SaveAdd, Next: store.Connection{Name: "box"}}
	if got, ok := Route(SaveMsg{Save: &save}); !ok || got != &save {
		t.Fatalf("save message = %+v, ok %t", got, ok)
	}
	if got, ok := Route(SaveMsg{}); !ok || got != nil {
		t.Fatalf("cancel message = %+v, ok %t", got, ok)
	}
}
