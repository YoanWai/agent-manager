package spawn

import "testing"

// The picker lists the root first and every live group under it, skipping a
// group whose ancestor is archived, and highlights the asked-for path.
func TestOptionsWalkTheLiveGroupTree(t *testing.T) {
	t.Parallel()
	groups := []string{"web", "web/api", "old", "old/deep"}
	options, index := Options(groups, []string{"web/api"}, func(path string) bool { return path == "old" }, "web/api")

	want := []string{"", "web", "web/api"}
	if len(options) != len(want) {
		t.Fatalf("options = %+v, want %v", options, want)
	}
	for i, path := range want {
		if options[i].Path != path {
			t.Fatalf("options = %+v, want %v", options, want)
		}
	}
	if index != 2 {
		t.Fatalf("highlight = %d, want web/api at 2", index)
	}
}

// A path that is not in the tree leaves the highlight on the root.
func TestOptionsFallBackToTheRoot(t *testing.T) {
	t.Parallel()
	options, index := Options(nil, nil, func(string) bool { return false }, "gone")
	if len(options) != 1 || options[0].Path != "" || index != 0 {
		t.Fatalf("options = %+v at %d, want the bare root", options, index)
	}
}
