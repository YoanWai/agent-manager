package ui

import "testing"

// A settled wordmark keeps a timer alive for the next shimmer, and the
// shimmer message starts the sweep over.
func TestBannerShimmerReArms(t *testing.T) {
	m := &Model{bannerPhase: bannerFrames}
	if m.bannerTick() == nil {
		t.Fatal("a settled banner should wait for the next shimmer, not stop")
	}
	updated, cmd := m.Update(bannerShimmerMsg{})
	m = updated.(*Model)
	if m.bannerPhase != 0 {
		t.Fatalf("shimmer should restart the sweep, phase = %d", m.bannerPhase)
	}
	if cmd == nil {
		t.Fatal("a restarted sweep must schedule its frames")
	}
}

// The resting wordmark spans the mark's gradient: the two ends must differ,
// darker teal on the left.
func TestBannerRestGradient(t *testing.T) {
	left := bannerRest(0)
	right := bannerRest(float64(bannerWidth() - 1))
	if left == right {
		t.Fatal("gradient ends should differ")
	}
}
