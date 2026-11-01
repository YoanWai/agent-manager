package focus

import "testing"

func hyperlink(target, label string) string {
	return "\x1b]8;id=a1;" + target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func TestHyperlinkClicksOpenTheTarget(t *testing.T) {
	model := mouseModel(t, []string{
		"Created " + hyperlink("https://example.atlassian.net/browse/GMN-3840", "GMN-3840 keep Dekel") + ". Done.",
		"see " + hyperlink("file:///tmp/notes.md", "notes.md") + " and " + hyperlink("javascript:alert(1)", "this"),
		"bare " + hyperlink("https://example.com/930", "https://example.com/930"),
		hyperlink("https://example.com/\u009b31m", "C1 control") + " " + hyperlink("https:///nohost", "no host"),
	}, 80, PaneUpdate{})

	cases := []struct {
		name string
		x, y int
		want string
	}{
		{"markdown label", 12, 0, "https://example.atlassian.net/browse/GMN-3840"},
		{"text after the label", 30, 0, ""},
		{"file link", 6, 1, ""},
		{"unsupported scheme", 19, 1, ""},
		{"bare URL the agent linked", 10, 2, "https://example.com/930"},
		{"control rune in the target", 2, 3, ""},
		{"target without a host", 14, 3, ""},
	}
	for _, c := range cases {
		if got := clickURL(&model, testOriginX+c.x, testOriginY+c.y); got != c.want {
			t.Errorf("%s: opened %q, want %q", c.name, got, c.want)
		}
	}

	model.TrackPointer(testOriginX+12, testOriginY)
	if !model.OverLink() {
		t.Fatal("hovering a hyperlink label is not over a link")
	}
}

// tmux closes a hyperlink at the end of each captured row, so the row a
// full-width link wrapped onto arrives with no link of its own.
func TestHyperlinkFollowsItsWrap(t *testing.T) {
	model := mouseModel(t, []string{
		"ab" + hyperlink("https://example.com/x", "LINKLINKLINKLINKLI"),
		"NKLINK zz",
	}, 20, PaneUpdate{})
	if got := clickURL(&model, testOriginX+2, testOriginY+1); got != "https://example.com/x" {
		t.Fatalf("wrapped part of the label opened %q", got)
	}
	if got := clickURL(&model, testOriginX+8, testOriginY+1); got != "" {
		t.Fatalf("text after the wrapped label opened %q", got)
	}
}

func TestWrappedURLOpensFromEveryRow(t *testing.T) {
	model := mouseModel(t, []string{
		"https://example.com/",
		"aaaaaaaaaaaaaaaaaaaa",
		"tail. more text",
	}, 20, PaneUpdate{})
	want := "https://example.com/aaaaaaaaaaaaaaaaaaaatail"
	for row := range 3 {
		if got := clickURL(&model, testOriginX+2, testOriginY+row); got != want {
			t.Errorf("row %d opened %q, want %q", row, got, want)
		}
	}
	if got := clickURL(&model, testOriginX+8, testOriginY+2); got != "" {
		t.Fatalf("text after the wrapped URL opened %q", got)
	}
}
