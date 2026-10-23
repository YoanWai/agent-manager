package frame

import "testing"

func TestClampPadsWithTheCallersRow(t *testing.T) {
	got := Clamp("x", 2, func() string { return "#" })
	if got != "x\n#" {
		t.Fatalf("Clamp = %q, want the caller's row under a short frame", got)
	}
}
