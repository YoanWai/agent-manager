package focus

import "testing"

func TestBottomWindow(t *testing.T) {
	capture := "a\nb\nc\nd\ne\n"
	cases := []struct {
		rows, offset int
		want         string
	}{
		{2, 0, "d\ne\n"},
		{2, 1, "c\nd\n"},
		{3, 4, "a\n"},
		{2, 9, "\n"},
	}
	for _, c := range cases {
		if got := BottomWindow(capture, c.rows, c.offset); got != c.want {
			t.Errorf("BottomWindow(rows %d, offset %d) = %q, want %q", c.rows, c.offset, got, c.want)
		}
	}
}
