package store

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
)

func connectionNames(t *testing.T, st *Store) []string {
	t.Helper()
	connections, err := st.Connections()
	if err != nil {
		t.Fatalf("connections: %v", err)
	}
	var names []string
	for _, c := range connections {
		names = append(names, c.Name)
	}
	return names
}

func TestConnectionsListInTheOrderAdded(t *testing.T) {
	st := newTestStore(t)
	for _, c := range []Connection{
		{Name: "zeta", Destination: "me@zeta"},
		{Name: "alpha", Destination: "alpha-alias"},
		{Name: "mid", Destination: "me@mid"},
	} {
		if err := st.AddConnection(c); err != nil {
			t.Fatalf("add %s: %v", c.Name, err)
		}
	}
	got, err := st.Connections()
	if err != nil {
		t.Fatalf("connections: %v", err)
	}
	want := []Connection{
		{Name: "zeta", Destination: "me@zeta"},
		{Name: "alpha", Destination: "alpha-alias"},
		{Name: "mid", Destination: "me@mid"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("connections = %v, want %v", got, want)
	}
}

func TestAddConnectionRefusesATakenName(t *testing.T) {
	st := newTestStore(t)
	if err := st.AddConnection(Connection{Name: "box", Destination: "me@one"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	err := st.AddConnection(Connection{Name: "box", Destination: "me@two"})
	if !errors.Is(err, ErrConnectionTaken) {
		t.Fatalf("duplicate add err = %v, want ErrConnectionTaken", err)
	}
	got, _ := st.Connections()
	if len(got) != 1 || got[0].Destination != "me@one" {
		t.Fatalf("connections after refused add = %v", got)
	}
}

func TestUpdateConnectionKeepsItsPlace(t *testing.T) {
	st := newTestStore(t)
	for _, name := range []string{"a", "b", "c"} {
		if err := st.AddConnection(Connection{Name: name, Destination: name + "-host"}); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
	}
	if err := st.UpdateConnection("b", Connection{Name: "renamed", Destination: "new-host"}); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := st.Connections()
	want := []Connection{{"a", "a-host"}, {"renamed", "new-host"}, {"c", "c-host"}}
	if !slices.Equal(got, want) {
		t.Fatalf("connections = %v, want %v", got, want)
	}
	if err := st.UpdateConnection("renamed", Connection{Name: "renamed", Destination: "other"}); err != nil {
		t.Fatalf("update destination only: %v", err)
	}
	if got, _ := st.Connections(); got[1] != (Connection{"renamed", "other"}) {
		t.Fatalf("after destination change = %v", got)
	}
}

func TestUpdateConnectionRefusesRenameOntoATakenName(t *testing.T) {
	st := newTestStore(t)
	st.AddConnection(Connection{Name: "a", Destination: "a-host"})
	st.AddConnection(Connection{Name: "b", Destination: "b-host"})
	err := st.UpdateConnection("a", Connection{Name: "b", Destination: "x"})
	if !errors.Is(err, ErrConnectionTaken) {
		t.Fatalf("rename onto taken err = %v, want ErrConnectionTaken", err)
	}
	got, _ := st.Connections()
	want := []Connection{{"a", "a-host"}, {"b", "b-host"}}
	if !slices.Equal(got, want) {
		t.Fatalf("connections = %v, want %v", got, want)
	}
}

func TestUpdateAndDeleteReportAMissingConnection(t *testing.T) {
	st := newTestStore(t)
	if err := st.UpdateConnection("ghost", Connection{Name: "ghost", Destination: "x"}); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("update missing err = %v", err)
	}
	if err := st.DeleteConnection("ghost"); !errors.Is(err, ErrConnectionNotFound) {
		t.Fatalf("delete missing err = %v", err)
	}
}

func TestDeleteConnectionLeavesTheRestInOrder(t *testing.T) {
	st := newTestStore(t)
	for _, name := range []string{"a", "b", "c"} {
		st.AddConnection(Connection{Name: name, Destination: name})
	}
	if err := st.DeleteConnection("b"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := st.AddConnection(Connection{Name: "d", Destination: "d"}); err != nil {
		t.Fatalf("add after delete: %v", err)
	}
	if got := connectionNames(t, st); !slices.Equal(got, []string{"a", "c", "d"}) {
		t.Fatalf("names = %v", got)
	}
}

func TestConnectionsSurviveReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := st.AddConnection(Connection{Name: "box", Destination: "me@box"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	st.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { reopened.Close() })
	if got := connectionNames(t, reopened); !slices.Equal(got, []string{"box"}) {
		t.Fatalf("names after reopen = %v", got)
	}
}
