package runtime

import "testing"

func TestWindowTakesRowsUpFromTheBottom(t *testing.T) {
	t.Parallel()
	capture := "one\ntwo\nthree\nfour\nfive\n"
	if got := Window(capture, 2, 1); got != "four\nfive\n" {
		t.Fatalf("Window = %q, want the last two rows", got)
	}
	if got := Window(capture, 2, 4); got != "one\n" {
		t.Fatalf("Window = %q, want the clamped first row", got)
	}
}
