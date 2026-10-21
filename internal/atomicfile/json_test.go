package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJSONRoundTripsAndRefusesWhatDoesNotDecode(t *testing.T) {
	type entry struct{ Name string }
	path := filepath.Join(t.TempDir(), "cache.json")
	if _, ok := ReadJSON[entry](path); ok {
		t.Fatal("a missing file read as ok")
	}
	if err := WriteJSON(path, entry{Name: "a"}, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadJSON[entry](path); !ok || got.Name != "a" {
		t.Fatalf("ReadJSON = %+v, %v", got, ok)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadJSON[entry](path); ok || got != (entry{}) {
		t.Fatalf("truncated JSON read as %+v, %v", got, ok)
	}
}
