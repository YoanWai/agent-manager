package store

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func assertSessionPresets(t *testing.T, st *Store, want []SessionPreset) {
	t.Helper()
	got, err := st.SessionPresets()
	if err != nil {
		t.Fatalf("SessionPresets: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SessionPresets = %#v, want %#v", got, want)
	}
}

func TestSessionPresetsCreateListAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	assertSessionPresets(t, st, []SessionPreset{})
	presets := []SessionPreset{{Name: "Zulu", Instructions: "\t  literal\n指示\n\n "}, {Name: " Alpha ", Instructions: "task"}}
	for _, preset := range presets {
		if err := st.SaveSessionPreset("", preset); err != nil {
			t.Fatal(err)
		}
	}
	want := []SessionPreset{{Name: "Alpha", Instructions: "task"}, presets[0]}
	assertSessionPresets(t, st, want)
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertSessionPresets(t, st, want)
}

func TestSessionPresetsUpdateAndRename(t *testing.T) {
	st := newTestStore(t)
	if err := st.SaveSessionPreset("", SessionPreset{"original", "before"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveSessionPreset("original", SessionPreset{"original", "updated"}); err != nil {
		t.Fatal(err)
	}
	assertSessionPresets(t, st, []SessionPreset{{"original", "updated"}})
	if err := st.SaveSessionPreset("original", SessionPreset{" renamed ", "after"}); err != nil {
		t.Fatal(err)
	}
	assertSessionPresets(t, st, []SessionPreset{{"renamed", "after"}})
}

func TestSessionPresetsCollisionsPreserveBothRows(t *testing.T) {
	st := newTestStore(t)
	want := []SessionPreset{{"one", "first"}, {"two", "second"}}
	for _, p := range want {
		if err := st.SaveSessionPreset("", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SaveSessionPreset("", SessionPreset{"one", "overwrite"}); err == nil {
		t.Fatal("duplicate create succeeded")
	}
	assertSessionPresets(t, st, want)
	if err := st.SaveSessionPreset("one", SessionPreset{"two", "overwrite"}); err == nil {
		t.Fatal("duplicate rename succeeded")
	}
	assertSessionPresets(t, st, want)
}

func TestSessionPresetsDeleteAndStaleUpdate(t *testing.T) {
	st := newTestStore(t)
	if deleted, err := st.DeleteSessionPreset("absent"); err != nil || deleted {
		t.Fatalf("unknown delete = %v, %v", deleted, err)
	}
	if err := st.SaveSessionPreset("", SessionPreset{"original", "before"}); err != nil {
		t.Fatal(err)
	}
	if deleted, err := st.DeleteSessionPreset("original"); err != nil || !deleted {
		t.Fatalf("delete = %v, %v", deleted, err)
	}
	for _, name := range []string{"original", "renamed"} {
		if err := st.SaveSessionPreset("original", SessionPreset{name, "resurrected"}); err == nil {
			t.Fatal("deleted original was recreated")
		}
	}
	if err := st.SaveSessionPreset("unknown", SessionPreset{"new", "created"}); err == nil {
		t.Fatal("unknown original was created")
	}
	assertSessionPresets(t, st, []SessionPreset{})
}

func TestSessionPresetsValidation(t *testing.T) {
	invalid := []struct {
		name   string
		preset SessionPreset
	}{
		{"empty name", SessionPreset{"", "instructions"}},
		{"whitespace name", SessionPreset{" \t\n", "instructions"}},
		{"long name", SessionPreset{strings.Repeat("界", 61), "instructions"}},
		{"control name", SessionPreset{"line\nbreak", "instructions"}},
		{"edge control name", SessionPreset{"\tname", "instructions"}},
		{"del name", SessionPreset{"name\x7f", "instructions"}},
		{"invalid UTF8 name", SessionPreset{"bad\xff", "instructions"}},
		{"empty instructions", SessionPreset{"valid", ""}},
		{"whitespace instructions", SessionPreset{"valid", " \n\t\u2003"}},
		{"invalid UTF8 instructions", SessionPreset{"valid", "bad\xff"}},
		{"long instructions", SessionPreset{"valid", strings.Repeat("x", 64*1024+1)}},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			st := newTestStore(t)
			if err := st.SaveSessionPreset("", SessionPreset{"existing", "keep"}); err != nil {
				t.Fatal(err)
			}
			for _, previous := range []string{"", "existing"} {
				if err := st.SaveSessionPreset(previous, tc.preset); err == nil {
					t.Fatalf("invalid preset accepted for previous %q", previous)
				}
			}
			assertSessionPresets(t, st, []SessionPreset{{"existing", "keep"}})
		})
	}
	t.Run("inclusive limits", func(t *testing.T) {
		st := newTestStore(t)
		p := SessionPreset{strings.Repeat("界", 60), strings.Repeat("界", 21845) + "x"}
		if err := st.SaveSessionPreset("", p); err != nil {
			t.Fatal(err)
		}
		assertSessionPresets(t, st, []SessionPreset{p})
	})
}

func TestSessionPresetLookupIsExactLiteralAndFresh(t *testing.T) {
	st := newTestStore(t)
	original := SessionPreset{Name: "Role�", Instructions: "\t literal\r\n界 ⇥␍␛"}
	if err := st.SaveSessionPreset("", original); err != nil {
		t.Fatal(err)
	}
	got, found, err := st.SessionPreset(original.Name)
	if err != nil || !found || got != original {
		t.Fatalf("literal lookup: %+v %v %v", got, found, err)
	}
	for _, name := range []string{"missing", " Role�", "Role� ", "Role"} {
		got, found, err := st.SessionPreset(name)
		if err != nil || found || got != (SessionPreset{}) {
			t.Fatalf("missing/exact lookup %q: %+v %v %v", name, got, found, err)
		}
	}
	changed := SessionPreset{Name: "Renamed", Instructions: "updated\n\t"}
	if err := st.SaveSessionPreset(original.Name, changed); err != nil {
		t.Fatal(err)
	}
	if _, found, err := st.SessionPreset(original.Name); err != nil || found {
		t.Fatalf("stale rename lookup: %v %v", found, err)
	}
	if got, found, err := st.SessionPreset(changed.Name); err != nil || !found || got != changed {
		t.Fatalf("fresh lookup: %+v %v %v", got, found, err)
	}
	if _, err := st.DeleteSessionPreset(changed.Name); err != nil {
		t.Fatal(err)
	}
	if _, found, err := st.SessionPreset(changed.Name); err != nil || found {
		t.Fatalf("stale delete lookup: %v %v", found, err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := st.SessionPreset(changed.Name); err == nil || found {
		t.Fatal("database failure became missing lookup")
	}
}
func TestSessionPresetLookupDoesNotReadUnrelatedRows(t *testing.T) {
	st := newTestStore(t)
	for _, query := range []string{
		"DROP TABLE session_presets",
		"CREATE TABLE session_presets (name TEXT PRIMARY KEY, instructions TEXT)",
		"INSERT INTO session_presets VALUES ('selected','literal')",
		"INSERT INTO session_presets VALUES ('unrelated',NULL)",
	} {
		if _, err := st.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.SessionPresets(); err == nil {
		t.Fatal("fixture must fail catalog-wide decoding")
	}
	if got, found, err := st.SessionPreset("selected"); err != nil || !found || got.Instructions != "literal" {
		t.Fatalf("unrelated body affected lookup: %+v %v %v", got, found, err)
	}
	if _, found, err := st.SessionPreset("unrelated"); err == nil || found {
		t.Fatal("selected decoding error concealed")
	}
}
