package main

import (
	"os"
	"strings"
	"testing"
)

func TestCompact(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, "0"},
		{176, "176"},
		{999, "999"},
		{1000, "1k"},
		{1918, "1.9k"},
		{12500, "12.5k"},
		{100000, "100k"},
	} {
		if got := compact(tc.in); got != tc.want {
			t.Errorf("compact(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFillTrendshiftIsIdempotentAndScoped(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	original := "# Title\n\n<p><!-- trendshift:start --><!-- trendshift:end --></p>\n\nbody stays put\n"
	if err := os.WriteFile("README.md", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := fillTrendshift(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile("README.md"); string(got) != original {
		t.Fatalf("an empty badge changed the README:\n%s", got)
	}

	if err := fillTrendshift(`<a href="x">badge</a>`); err != nil {
		t.Fatal(err)
	}
	filled, _ := os.ReadFile("README.md")
	if !strings.Contains(string(filled), `<a href="x">badge</a>`) {
		t.Fatalf("badge was not inserted:\n%s", filled)
	}
	if !strings.Contains(string(filled), "body stays put") || !strings.HasPrefix(string(filled), "# Title") {
		t.Fatalf("text outside the markers was disturbed:\n%s", filled)
	}

	if err := fillTrendshift(""); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile("README.md"); string(got) != original {
		t.Fatalf("clearing the badge did not restore the region:\n%s", got)
	}
}

func TestFillTrendshiftNeedsMarkers(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("README.md", []byte("no markers here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fillTrendshift("badge"); err == nil {
		t.Fatal("a README without markers should be an error, not a silent no-op")
	}
}
