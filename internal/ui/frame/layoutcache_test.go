package frame

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

func TestLayoutCacheFollowsKeysAndTerminalWidth(t *testing.T) {
	var cache LayoutCache
	refs := [][]Section{{{Title: "Keys", Pairs: [][2]string{{"x", "go"}}}}}
	if got := cache.Measure(refs, 28, 3, presentation.GlyphModeAdvanced); got.Mode != Titled {
		t.Fatalf("initial layout=%v", got)
	}
	refs[0][0].Pairs[0] = [2]string{"alt+backspace", "complete"}
	if got := cache.Measure(refs, 28, 3, presentation.GlyphModeAdvanced); got.Mode != Worded {
		t.Fatalf("changed key kept stale layout=%v", got)
	}
	if got := cache.Measure(refs, 100, 3, presentation.GlyphModeAdvanced); got.Mode != Titled {
		t.Fatalf("wider terminal kept stale layout=%v", got)
	}
	if got := cache.Measure(refs, 10, 3, presentation.GlyphModeAdvanced); got.Mode != Bare {
		t.Fatalf("narrow terminal kept stale layout=%v", got)
	}
}

func TestLayoutCacheKeepsGlyphModesAndModelsIndependent(t *testing.T) {
	var first, second LayoutCache
	refs := [][]Section{{{Title: "Keys", Pairs: [][2]string{{"⌫", "delete"}}}}}
	for _, cache := range []*LayoutCache{&first, &second, &first} {
		if got := cache.Measure(refs, 24, 3, presentation.GlyphModeAdvanced); got.Mode != Titled {
			t.Fatalf("advanced layout=%v", got)
		}
	}
	if got := first.Measure(refs, 24, 3, presentation.GlyphModeBasic); got.Mode != Worded {
		t.Fatalf("compatible layout=%v", got)
	}
	if got := second.Measure(refs, 24, 3, presentation.GlyphModeAdvanced); got.Mode != Titled {
		t.Fatalf("other model inherited compatible layout=%v", got)
	}
}
