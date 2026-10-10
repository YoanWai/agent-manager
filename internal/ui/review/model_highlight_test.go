package review

import "testing"

// Eviction drops only the oldest entry, so an overflowing cache settles at
// its cap rather than below it.
func TestHLCacheEvicts(t *testing.T) {
	var model Model
	for i := 0; i < highlightCacheCap+3; i++ {
		model.putHighlight(HighlightKey{Path: string(rune('a' + i))}, &Highlight{})
	}
	if len(model.highlights) != highlightCacheCap {
		t.Fatalf("cache size = %d", len(model.highlights))
	}
}
