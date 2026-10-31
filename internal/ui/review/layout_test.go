package review

import "testing"

// The code pane takes what the frame and the legend leave, and a docked
// comment box takes its rows plus the blank row holding it off the code.
func TestCodeHeightLeavesRoomForTheFrameAndCommentBox(t *testing.T) {
	for _, tc := range []struct {
		name           string
		termHeight     int
		footerHeight   int
		annotationRows int
		want           int
	}{
		{name: "plain", termHeight: 40, footerHeight: 2, want: 30},
		{name: "comment box", termHeight: 40, footerHeight: 2, annotationRows: 3, want: 26},
		{name: "one row of comment still takes its blank row", termHeight: 40, footerHeight: 2, annotationRows: 1, want: 28},
		{name: "a terminal too short for the frame keeps one row", termHeight: 6, footerHeight: 2, want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CodeHeight(tc.termHeight, tc.footerHeight, tc.annotationRows); got != tc.want {
				t.Fatalf("CodeHeight = %d, want %d", got, tc.want)
			}
		})
	}
}
