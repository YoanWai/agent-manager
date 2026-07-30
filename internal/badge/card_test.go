package badge

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var stats = []Stat{
	{Value: "168", Label: "stars", Color: "#d08442"},
	{Value: "1.9k", Label: "clones · 14d", Color: "#a78bd0"},
	{Value: "v0.11.1", Label: "release", Color: "#6f9fd0"},
}

func TestCardCarriesEveryStat(t *testing.T) {
	svg := Card(stats, Dark)
	for _, stat := range stats {
		if !strings.Contains(svg, ">"+stat.Value+"<") {
			t.Errorf("card is missing the value %q", stat.Value)
		}
		if !strings.Contains(svg, ">"+strings.ToUpper(stat.Label)+"<") {
			t.Errorf("card is missing the label %q", stat.Label)
		}
	}
	if !strings.Contains(svg, `aria-label="168 stars, 1.9k clones · 14d, v0.11.1 release"`) {
		t.Errorf("card does not describe itself:\n%s", svg)
	}
}

// Centring every run is what frees the layout from guessing glyph advances, so
// a stretched or left-anchored run would be a regression.
func TestCardCentresEveryRun(t *testing.T) {
	svg := Card(stats, Light)
	runs := strings.Count(svg, "<text")
	if anchored := strings.Count(svg, `text-anchor="middle"`); anchored != runs {
		t.Fatalf("%d of %d runs are centred", anchored, runs)
	}
	if strings.Contains(svg, "textLength") {
		t.Fatal("card is pinning text to a guessed width")
	}
}

func TestCardColumnsAreWholeAndEven(t *testing.T) {
	svg := Card(stats, Dark)
	width := regexp.MustCompile(`width="([0-9.]+)"`).FindStringSubmatch(svg)
	if width == nil {
		t.Fatal("card has no width")
	}
	value, err := strconv.ParseFloat(width[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	if value != float64(int(value)) {
		t.Fatalf("card width %v is not a whole number of pixels", value)
	}
	if rules := strings.Count(svg, "<line"); rules != len(stats)-1 {
		t.Fatalf("got %d dividers for %d columns", rules, len(stats))
	}
}

func TestCardWithoutStatsRendersNothing(t *testing.T) {
	if Card(nil, Dark) != "" {
		t.Fatal("empty card still drew something")
	}
}
