package store

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func listIDs(t *testing.T, st *Store, includeArchived bool) []string {
	t.Helper()
	sessions, err := st.ListSessions(includeArchived)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	ids := make([]string, len(sessions))
	for i, sess := range sessions {
		ids[i] = sess.ID
	}
	return ids
}

func TestReorderSession(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("b", "g1"))
	st.CreateSession(sample("c", "g1"))

	if moved, err := st.ReorderSession("c", -1, false); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	got := listIDs(t, st, false)
	want := []string{"a", "c", "b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v want %v", got, want)
		}
	}

	// Top of group: no-op, no error.
	if moved, err := st.ReorderSession("a", -1, false); err != nil || moved {
		t.Fatalf("edge reorder: moved=%v err=%v, want no-op", moved, err)
	}
	if ids := listIDs(t, st, false); ids[0] != "a" {
		t.Fatalf("edge move should keep order, got %v", ids)
	}
}

func TestReorderSessionSkipsArchivedInActiveView(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("b", "g1"))
	st.CreateSession(sample("c", "g1"))
	st.SetArchived("b", true)

	if moved, err := st.ReorderSession("c", -1, false); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	got := listIDs(t, st, false)
	if got[0] != "c" || got[1] != "a" {
		t.Fatalf("c should jump over hidden b to swap with a, got %v", got)
	}
}

func TestSwapSessionOrderCrossesFilteredSibling(t *testing.T) {
	st := newTestStore(t)
	st.CreateSession(sample("a", "g1"))
	st.CreateSession(sample("hidden", "g1"))
	st.CreateSession(sample("c", "g1"))

	if err := st.SwapSessionOrder("c", "a"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	if got, want := listIDs(t, st, false), []string{"c", "hidden", "a"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v want %v", got, want)
	}
}

func TestReorderGroup(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("alpha", "")
	st.CreateGroup("beta", "")
	st.CreateGroup("alpha/sub", "")

	if moved, err := st.ReorderGroup("beta", -1); err != nil || !moved {
		t.Fatalf("reorder: moved=%v err=%v", moved, err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	posOf := func(name string) int {
		for i, g := range groups {
			if g.Name == name {
				return i
			}
		}
		return -1
	}
	if posOf("beta") > posOf("alpha") {
		t.Fatalf("beta should come before alpha, got %v", groups)
	}

	// Nested group only swaps with same-parent siblings; sole child is a no-op.
	if moved, err := st.ReorderGroup("alpha/sub", -1); err != nil || moved {
		t.Fatalf("nested sole child: moved=%v err=%v, want no-op", moved, err)
	}
}

func TestSwapGroupOrderCrossesFilteredSibling(t *testing.T) {
	st := newTestStore(t)
	st.CreateGroup("alpha", "")
	st.CreateGroup("hidden", "")
	st.CreateGroup("gamma", "")

	if err := st.SwapGroupOrder("gamma", "alpha"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	got := make([]string, len(groups))
	for i, group := range groups {
		got[i] = group.Name
	}
	if want := []string{"gamma", "hidden", "alpha"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v want %v", got, want)
	}
}

func TestSwapGroupOrderMaterializesSyntheticAncestors(t *testing.T) {
	st := newTestStore(t)
	for _, group := range []string{"alpha/deep", "beta/deep", "gamma/deep"} {
		if err := st.CreateGroup(group, ""); err != nil {
			t.Fatalf("create group %q: %v", group, err)
		}
	}

	if err := st.SwapGroupOrder("gamma", "beta", "alpha", "beta", "gamma"); err != nil {
		t.Fatalf("swap: %v", err)
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatalf("groups: %v", err)
	}
	var roots []string
	for _, group := range groups {
		if !strings.Contains(group.Name, "/") {
			roots = append(roots, group.Name)
		}
	}
	if want := []string{"alpha", "gamma", "beta"}; !slices.Equal(roots, want) {
		t.Fatalf("root order = %v want %v", roots, want)
	}
}

func TestReorderSessionStaysInSiblingSet(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	if err := st.CreateSession(sample("other", "g")); err != nil {
		t.Fatalf("other: %v", err)
	}
	a := sample("a", "g")
	a.ParentID = "agent"
	b := sample("b", "g")
	b.ParentID = "agent"
	if err := st.CreateSession(a); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := st.CreateSession(b); err != nil {
		t.Fatalf("b: %v", err)
	}
	moved, err := st.ReorderSession("a", 1, false)
	if err != nil || !moved {
		t.Fatalf("reorder child: moved=%v err=%v", moved, err)
	}
	kids, _ := st.Children("agent")
	if len(kids) != 2 || kids[0].ID != "b" || kids[1].ID != "a" {
		t.Fatalf("child order %+v", kids)
	}
	roots := []string{}
	for _, id := range listIDs(t, st, false) {
		if id == "agent" || id == "other" {
			roots = append(roots, id)
		}
	}
	if len(roots) != 2 || roots[0] != "agent" || roots[1] != "other" {
		t.Fatalf("un-nested order %v", roots)
	}
	if err := st.SwapSessionOrder("agent", "a"); err == nil {
		t.Fatal("agent and its child are not siblings")
	}
}

func TestPlaceSessionBeforeLandsAheadOfItsNewSibling(t *testing.T) {
	st := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := st.CreateSession(sample(id, "g2")); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if err := st.CreateSession(sample("mover", "g1")); err != nil {
		t.Fatalf("mover: %v", err)
	}
	if err := st.PlaceSessionBefore("mover", "b"); err != nil {
		t.Fatalf("place: %v", err)
	}
	got, err := siblingOrder(st.db, "g2", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "mover", "b", "c"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestPlaceSessionBeforeReordersWithinItsOwnGroup(t *testing.T) {
	st := newTestStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if err := st.CreateSession(sample(id, "g")); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if err := st.PlaceSessionBefore("c", "a"); err != nil {
		t.Fatalf("place: %v", err)
	}
	got, err := siblingOrder(st.db, "g", "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"c", "a", "b"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestPlaceSessionBeforeAGoneSessionSaysSo(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("mover", "g")); err != nil {
		t.Fatalf("mover: %v", err)
	}
	err := st.PlaceSessionBefore("mover", "vanished")
	if !errors.Is(err, ErrSessionGone) {
		t.Fatalf("PlaceSessionBefore(gone) = %v, want ErrSessionGone", err)
	}
}

// A move the store refuses leaves the list exactly as it was: the placement
// and the ordering commit together or not at all.
func TestPlaceSessionBeforeRefusedMovesNothing(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateSession(sample("agent", "g1")); err != nil {
		t.Fatalf("agent: %v", err)
	}
	kid := sample("kid", "g1")
	kid.ParentID = "agent"
	if err := st.CreateSession(kid); err != nil {
		t.Fatalf("kid: %v", err)
	}
	if err := st.CreateSession(sample("host", "g2")); err != nil {
		t.Fatalf("host: %v", err)
	}
	nested := sample("nested", "g2")
	nested.ParentID = "host"
	if err := st.CreateSession(nested); err != nil {
		t.Fatalf("nested: %v", err)
	}
	if err := st.PlaceSessionBefore("agent", "nested"); err == nil {
		t.Fatal("an agent with terminals of its own cannot nest under another")
	}
	got, err := st.Get("agent")
	if err != nil || got.Group != "g1" || got.ParentID != "" {
		t.Fatalf("agent moved anyway: %+v err %v", got, err)
	}
}
