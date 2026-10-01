package store

import "testing"

func TestRestoreRollsBackRowWhenAncestorWriteFails(t *testing.T) {
	st := newTestStore(t)
	if err := st.CreateGroup("zone", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateGroup("zone/inner", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(sample("cafe", "zone/inner")); err != nil {
		t.Fatal(err)
	}
	if err := st.SetGroupArchived("zone", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER refuse_ancestor BEFORE UPDATE OF archived ON groups WHEN NEW.name = 'zone' BEGIN SELECT RAISE(ABORT, 'ancestor refused'); END`); err != nil {
		t.Fatal(err)
	}
	if err := st.SetArchived("cafe", false); err == nil {
		t.Fatal("expected ancestor failure")
	}
	row, err := st.Get("cafe")
	if err != nil {
		t.Fatal(err)
	}
	if !row.Archived {
		t.Fatal("failed restore committed the row before ancestors")
	}
	groups, err := st.Groups()
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range groups {
		if !group.Archived {
			t.Fatalf("failed restore changed ancestor %s", group.Name)
		}
	}
}
