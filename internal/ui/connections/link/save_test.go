package link

import (
	"errors"
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
)

// connectionsTable is the connections table a save writes.
type connectionsTable struct {
	rows   []store.Connection
	refuse error
}

func (c *connectionsTable) AddConnection(next store.Connection) error {
	if c.refuse != nil {
		return c.refuse
	}
	c.rows = append(c.rows, next)
	return nil
}

func (c *connectionsTable) UpdateConnection(name string, next store.Connection) error {
	if c.refuse != nil {
		return c.refuse
	}
	for i, row := range c.rows {
		if row.Name == name {
			c.rows[i] = next
		}
	}
	return nil
}

func (c *connectionsTable) DeleteConnection(name string) error {
	if c.refuse != nil {
		return c.refuse
	}
	c.rows = slices.DeleteFunc(c.rows, func(row store.Connection) bool { return row.Name == name })
	return nil
}

func (c *connectionsTable) Connections() ([]store.Connection, error) {
	if c.refuse != nil {
		return nil, c.refuse
	}
	return slices.Clone(c.rows), nil
}

func TestSaveWritesTheListAndReadsItBack(t *testing.T) {
	table := &connectionsTable{}
	box := store.Connection{Name: "box", Destination: "me@box"}
	if _, err := Save(table, SaveRequest{Op: SaveAdd, Next: box}); err != nil {
		t.Fatalf("add: %v", err)
	}
	list, err := Save(table, SaveRequest{Op: SaveUpdate, Name: "box", Next: store.Connection{Name: "lab", Destination: "me@box"}})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(list) != 1 || list[0].Name != "lab" {
		t.Fatalf("list = %+v after the rename", list)
	}
	if list, err := Save(table, SaveRequest{Op: SaveRemove, Name: "lab"}); err != nil || len(list) != 0 {
		t.Fatalf("remove: list %+v, err %v", list, err)
	}
}

// A refused write keeps the card open and says why; nothing is adopted.
func TestAFailedSaveKeepsTheCardAndSaysWhy(t *testing.T) {
	refused := errors.New("UNIQUE constraint failed: connections.name")
	state := New(remote.New(t.TempDir()), nil)
	root := &recorder{}
	cmd := state.ApplySave(root, SaveRequest{Op: SaveAdd, Next: store.Connection{Name: "box"}, Gen: 7}, nil, refused)
	if cmd != nil {
		t.Fatal("a failed save went on to poll")
	}
	if !slices.Equal(root.answered, []bool{true}) {
		t.Fatalf("answered = %v, want the card told the save failed", root.answered)
	}
	if want := "connection not saved: " + refused.Error(); !slices.Equal(root.errs, []string{want}) {
		t.Fatalf("errors = %q, want %q", root.errs, want)
	}
	if len(state.List()) != 0 {
		t.Fatalf("list = %+v after a failed save", state.List())
	}
}

// A rename moves the rail's row to the new name; a removal forgets it.
func TestASavedListTellsTheRailWhatMoved(t *testing.T) {
	state := New(remote.New(t.TempDir()), nil)
	added := []store.Connection{{Name: "lab", Destination: "me@box"}}

	root := &recorder{}
	if cmd := state.ApplySave(root, SaveRequest{Op: SaveUpdate, Name: "box", Next: store.Connection{Name: "lab"}}, added, nil); cmd == nil {
		t.Fatal("a saved connection was not polled")
	}
	if want := (RailChange{Rename: "box", To: "lab", Focus: "lab"}); len(root.rail) != 1 || root.rail[0] != want {
		t.Fatalf("rail = %+v, want %+v", root.rail, want)
	}
	if !slices.Equal(root.done, []string{"connecting to lab over SSH"}) {
		t.Fatalf("status = %q", root.done)
	}

	root = &recorder{}
	if cmd := state.ApplySave(root, SaveRequest{Op: SaveRemove, Name: "lab"}, nil, nil); cmd != nil {
		t.Fatal("a removed connection was polled")
	}
	if want := (RailChange{Forget: "lab"}); len(root.rail) != 1 || root.rail[0] != want {
		t.Fatalf("rail = %+v, want %+v", root.rail, want)
	}
}
