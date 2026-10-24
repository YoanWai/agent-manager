package panes

import (
	"testing"
)

// A resize request copies its targets, so the selection that queued it can
// keep building on the slice it passed in.
func TestResizeRequestCopiesItsTargets(t *testing.T) {
	t.Parallel()
	request := ResizeRequest{Resize: Resize{Targets: []PaneResize{{ID: "s1", Size: [2]int{80, 24}}}}}
	copied := request.EffectCopy().(ResizeRequest)
	copied.Targets[0] = PaneResize{ID: "other", Size: [2]int{1, 1}}
	if request.Targets[0].ID != "s1" {
		t.Fatal("the copy shared the targets slice with the request")
	}
	if copied.EffectName() != "pane resize" {
		t.Fatalf("name = %q", copied.EffectName())
	}
}

// A resize runs against the services the package declares: the box is
// published and every target pinned.
func TestResizeRequestRunsAgainstItsServices(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	driver := &fakeDriver{}
	result, err := ResizeRequest{Resize: Resize{
		Publish: [2]int{100, 30},
		Targets: []PaneResize{{ID: "s1", Size: [2]int{100, 30}}},
	}}.Run(Services{Store: st, Driver: driver})
	if err != nil {
		t.Fatalf("resize: %v", err)
	}
	if len(st.panes) != 1 || st.panes[0] != [2]int{100, 30} {
		t.Fatalf("published %v, want the request's box", st.panes)
	}
	if driver.sizes["s1"] != [2]int{100, 30} {
		t.Fatalf("pinned %v, want the request's box", driver.sizes)
	}
	landed, ok := result.(Result)
	if !ok {
		t.Fatalf("result = %T, want Result", result)
	}
	if landed.Publish != [2]int{100, 30} {
		t.Fatalf("published box = %v, want the request's", landed.Publish)
	}
}

// A ratio save writes the setting, and reports the manager built without a
// store rather than panicking on it.
func TestSplitSaveRequestWritesTheRatio(t *testing.T) {
	t.Parallel()
	st := &fakeStore{}
	result, err := SplitSaveRequest{Value: "0.4200"}.Run(Services{Store: st})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if result != nil {
		t.Fatalf("result = %T, want nothing", result)
	}
	if st.settings["split_ratio"] != "0.4200" {
		t.Fatalf("stored ratio = %q", st.settings["split_ratio"])
	}
	if _, err := (SplitSaveRequest{Value: "0.5"}).Run(Services{}); err != NoStore {
		t.Fatalf("a storeless save reported %v, want %v", err, NoStore)
	}
}
