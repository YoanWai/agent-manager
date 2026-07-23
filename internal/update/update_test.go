package update

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in    string
		want  [3]int
		valid bool
	}{
		{"v0.8.2", [3]int{0, 8, 2}, true},
		{"0.8.2", [3]int{0, 8, 2}, true},
		{"v1.20.30", [3]int{1, 20, 30}, true},
		{"v0.9.0-12-gabc", [3]int{0, 9, 0}, true},
		{"dev", [3]int{}, false},
		{"v0.8", [3]int{}, false},
		{"v0.8.x", [3]int{}, false},
		{"", [3]int{}, false},
	}
	for _, c := range cases {
		got, ok := parseVersion(c.in)
		if ok != c.valid || got != c.want {
			t.Errorf("parseVersion(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.valid)
		}
	}
}

func TestGreater(t *testing.T) {
	cases := []struct {
		a, b [3]int
		want bool
	}{
		{[3]int{0, 9, 0}, [3]int{0, 8, 2}, true},
		{[3]int{1, 0, 0}, [3]int{0, 99, 99}, true},
		{[3]int{0, 8, 3}, [3]int{0, 8, 2}, true},
		{[3]int{0, 8, 2}, [3]int{0, 8, 2}, false},
		{[3]int{0, 8, 1}, [3]int{0, 8, 2}, false},
	}
	for _, c := range cases {
		if got := greater(c.a, c.b); got != c.want {
			t.Errorf("greater(%v,%v) = %v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNewerThan(t *testing.T) {
	if r := newerThan([3]int{0, 8, 2}, "v0.9.0", "url"); r.Latest != "v0.9.0" {
		t.Errorf("expected newer release, got %+v", r)
	}
	if r := newerThan([3]int{0, 8, 2}, "v0.8.2", "url"); r.Latest != "" {
		t.Errorf("equal version should not badge, got %+v", r)
	}
	if r := newerThan([3]int{0, 8, 2}, "garbage", "url"); r.Latest != "" {
		t.Errorf("unparseable latest should not badge, got %+v", r)
	}
}

func TestCheckDevBuildSkips(t *testing.T) {
	if r, err := Check(context.Background(), t.TempDir(), "dev"); err != nil || r.Latest != "" {
		t.Errorf("dev build should skip: got %+v err %v", r, err)
	}
}

// A fresh cache short-circuits the network entirely; seeding one lets us
// exercise Check's decision path without hitting GitHub.
func TestCheckUsesFreshCache(t *testing.T) {
	dir := t.TempDir()
	seed := cache{CheckedAt: time.Now(), Latest: "v0.9.0", URL: "https://example/rel"}
	raw, _ := json.Marshal(seed)
	if err := os.WriteFile(filepath.Join(dir, cacheFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Latest != "v0.9.0" || r.URL != "https://example/rel" {
		t.Errorf("expected cached newer release, got %+v", r)
	}
}

func TestCheckFreshCacheNoUpdate(t *testing.T) {
	dir := t.TempDir()
	seed := cache{CheckedAt: time.Now(), Latest: "v0.8.2", URL: "u"}
	raw, _ := json.Marshal(seed)
	os.WriteFile(filepath.Join(dir, cacheFile), raw, 0o644)
	r, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || r.Latest != "" {
		t.Errorf("same version should not badge: got %+v err %v", r, err)
	}
}
