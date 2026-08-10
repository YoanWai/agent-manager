package main

import (
	"image"
	"image/color"
	"os"
	"testing"
)

func TestFillContributors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	original := "# Title\n\n<!-- contributors:start -->old<!-- contributors:end -->\n\nbody stays put\n"
	if err := os.WriteFile("README.md", []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	contributors := humanContributors([]contributor{
		{Login: "one", HTMLURL: "https://github.com/one", Type: "User"},
		{Login: "robot", HTMLURL: "https://github.com/robot", Type: "Bot"},
		{Login: "two", HTMLURL: "https://github.com/two", Type: "User"},
	})
	if err := fillContributors(contributors); err != nil {
		t.Fatal(err)
	}

	want := "# Title\n\n<!-- contributors:start -->\n" +
		`<a href="https://github.com/one"><img src="docs/badges/contributors/one.png" width="64" height="64" alt="@one"></a>` + " " +
		`<a href="https://github.com/two"><img src="docs/badges/contributors/two.png" width="64" height="64" alt="@two"></a>` + "\n" +
		"<!-- contributors:end -->\n\nbody stays put\n"
	got, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("unexpected README:\n%s", got)
	}
	if err := fillContributors(contributors); err != nil {
		t.Fatal(err)
	}
	again, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != want {
		t.Fatalf("a second fill changed the README:\n%s", again)
	}
}

func TestFillContributorsNeedsMarkers(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("README.md", []byte("no markers here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fillContributors(nil); err == nil {
		t.Fatal("a README without markers should be an error, not a silent no-op")
	}
}

func TestAddContributorsDeduplicates(t *testing.T) {
	one := contributor{Login: "one"}
	two := contributor{Login: "two"}
	got := addContributors([]contributor{one}, []contributor{one, two})
	if len(got) != 2 || got[0].Login != "one" || got[1].Login != "two" {
		t.Fatalf("addContributors() = %#v", got)
	}
}

func TestWriteContributorAvatars(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	avatarDir := "docs/badges/contributors"
	if err := os.MkdirAll(avatarDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(avatarDir+"/stale.png", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	contributors := []contributor{{Login: "one"}, {Login: "two"}}
	if err := writeContributorAvatars(contributors, [][]byte{[]byte("first"), []byte("second")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(avatarDir + "/stale.png"); !os.IsNotExist(err) {
		t.Fatalf("stale avatar still exists: %v", err)
	}
	for name, want := range map[string]string{"one.png": "first", "two.png": "second"} {
		got, err := os.ReadFile(avatarDir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestRoundAvatar(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 128, 128))
	for y := range 128 {
		for x := range 128 {
			source.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
		}
	}
	avatar := roundAvatar(source)
	if avatar.Bounds() != image.Rect(0, 0, avatarSize, avatarSize) {
		t.Fatalf("avatar bounds = %v", avatar.Bounds())
	}
	if alpha := avatar.NRGBAAt(0, 0).A; alpha != 0 {
		t.Fatalf("corner alpha = %d, want 0", alpha)
	}
	if alpha := avatar.NRGBAAt(avatarSize/2, avatarSize/2).A; alpha != 255 {
		t.Fatalf("center alpha = %d, want 255", alpha)
	}
}
