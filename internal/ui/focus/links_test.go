package focus

import "testing"

func hyperlink(target, label string) string {
	return "\x1b]8;id=a1;" + target + "\x1b\\" + label + "\x1b]8;;\x1b\\"
}

func TestHyperlinkClicksOpenTheTarget(t *testing.T) {
	model := mouseModel(t, []string{
		"Created " + hyperlink("https://example.com/issues/42", "issue 42, fix login") + ". Done.",
		"see " + hyperlink("file:///tmp/notes.md", "notes.md") + " and " + hyperlink("javascript:alert(1)", "this"),
		"bare " + hyperlink("https://example.com/930", "https://example.com/930"),
		hyperlink("https://example.com/\u009b31m", "C1 control") + " " + hyperlink("https:///nohost", "no host") + " " + hyperlink("file://otherhost/x", "remote file"),
	}, 80, PaneUpdate{})

	cases := []struct {
		name string
		x, y int
		want string
	}{
		{"markdown label", 12, 0, "https://example.com/issues/42"},
		{"text after the label", 30, 0, ""},
		{"file link", 6, 1, "file:///tmp/notes.md"},
		{"unsupported scheme", 19, 1, ""},
		{"bare URL the agent linked", 10, 2, "https://example.com/930"},
		{"control rune in the target", 2, 3, ""},
		{"target without a host", 14, 3, ""},
		{"file on another host", 22, 3, ""},
	}
	for _, c := range cases {
		if got := clickURL(&model, testOriginX+c.x, testOriginY+c.y); got != c.want {
			t.Errorf("%s: opened %q, want %q", c.name, got, c.want)
		}
	}

	model.TrackPointer(testOriginX+12, testOriginY)
	if got := model.HoveredLink(); got != "https://example.com/issues/42" {
		t.Fatalf("hovering a hyperlink label reads %q", got)
	}
}

// The preview opens a link straight away only when its text is its address,
// since nothing there names a hidden target before the click.
func TestPaneLinksOpenOnlyShownAddresses(t *testing.T) {
	links := ReadPaneLinks([]string{
		"read " + hyperlink("https://example.com/docs", "the docs"),
		"then https://example.com/next",
		"bare " + hyperlink("https://example.com/930", "https://example.com/930"),
	}, Box{X: 40, Y: 10, Width: 60, Height: 3, Valid: true})
	cases := []struct {
		x, y int
		want string
	}{
		{46, 10, ""},
		{52, 11, "https://example.com/next"},
		{50, 12, "https://example.com/930"},
		{41, 11, ""},
		{6, 11, ""},
		{52, 13, ""},
	}
	for _, c := range cases {
		if got := links.ShownAt(c.x, c.y); got != c.want {
			t.Errorf("ShownAt(%d,%d) = %q, want %q", c.x, c.y, got, c.want)
		}
	}
	if got := (PaneLinks{}).ShownAt(0, 0); got != "" {
		t.Fatalf("a pane that painted nothing has link %q", got)
	}
}
