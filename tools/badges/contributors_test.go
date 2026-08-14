package main

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
)

func TestAddContributorsDeduplicates(t *testing.T) {
	one := contributor{Login: "one"}
	two := contributor{Login: "two"}
	got := addContributors([]contributor{one}, []contributor{one, two})
	if len(got) != 2 || got[0].Login != "one" || got[1].Login != "two" {
		t.Fatalf("addContributors() = %#v", got)
	}
}

func TestWriteContributorImage(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	red := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	blue := image.NewNRGBA(image.Rect(0, 0, avatarSize, avatarSize))
	for y := range avatarSize {
		for x := range avatarSize {
			red.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			blue.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
		}
	}
	if err := writeContributorImage([]*image.NRGBA{red, blue}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(outDir, "contributors.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	got, _, err := image.Decode(file)
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != image.Rect(0, 0, avatarSize*2+avatarGap, avatarSize) {
		t.Fatalf("image bounds = %v", got.Bounds())
	}
	if pixel := color.NRGBAModel.Convert(got.At(avatarSize/2, avatarSize/2)).(color.NRGBA); pixel.R != 255 || pixel.A != 255 {
		t.Fatalf("first avatar pixel = %#v", pixel)
	}
	if pixel := color.NRGBAModel.Convert(got.At(avatarSize+avatarGap+avatarSize/2, avatarSize/2)).(color.NRGBA); pixel.B != 255 || pixel.A != 255 {
		t.Fatalf("second avatar pixel = %#v", pixel)
	}
	if pixel := color.NRGBAModel.Convert(got.At(avatarSize, avatarSize/2)).(color.NRGBA); pixel.A != 0 {
		t.Fatalf("gap pixel = %#v", pixel)
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
