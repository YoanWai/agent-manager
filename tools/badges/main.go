// Command badges regenerates the README's status chips from live repo data.
// It runs in CI, so the numbers in the README are the numbers GitHub reports
// rather than a figure someone typed once and forgot.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/badge"
)

const (
	repo   = "YoanWai/agent-manager"
	outDir = "docs/badges"
)

const (
	amber  = "#d08442"
	blue   = "#6f9fd0"
	purple = "#a78bd0"
	red    = "#cc6a6a"
	subtle = "#7d8590"
)

// lightInk darkens the dark-theme accents enough to clear 4.5:1 against a
// white surface, so the same chip reads in either GitHub theme.
var lightInk = map[string]string{
	amber:  "#96591f",
	blue:   "#2f5f8f",
	purple: "#6a4a94",
	red:    "#a33c3c",
	subtle: "#59636e",
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "badges:", err)
		os.Exit(1)
	}
}

func run() error {
	stats, err := collect()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	light := make([]badge.Stat, len(stats))
	for i, stat := range stats {
		light[i] = stat
		light[i].Color = lightInk[stat.Color]
	}
	if err := write("stats-light.svg", badge.Card(light, badge.Light)); err != nil {
		return err
	}
	return write("stats-dark.svg", badge.Card(stats, badge.Dark))
}

func write(name, svg string) error {
	return os.WriteFile(filepath.Join(outDir, name), []byte(svg+"\n"), 0o644)
}

func collect() ([]badge.Stat, error) {
	var repoInfo struct {
		Stars   int `json:"stargazers_count"`
		License struct {
			SPDX string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := get("https://api.github.com/repos/"+repo, &repoInfo); err != nil {
		return nil, err
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := get("https://api.github.com/repos/"+repo+"/releases?per_page=1", &releases); err != nil {
		return nil, err
	}
	latest := ""
	if len(releases) > 0 {
		latest = releases[0].TagName
	}

	clones := cloneCount()

	license := repoInfo.License.SPDX
	if license == "" {
		license = "MIT"
	}

	stats := []badge.Stat{{Value: compact(repoInfo.Stars), Label: "stars", Color: amber}}
	if clones > 0 {
		stats = append(stats, badge.Stat{Value: compact(clones), Label: "clones · 14d", Color: purple})
	}
	return append(stats,
		badge.Stat{Value: latest, Label: "release", Color: blue},
		badge.Stat{Value: license, Label: "license", Color: subtle},
	), nil
}

// cloneCount reads 14-day clone traffic. The endpoint needs push access, which
// the Actions GITHUB_TOKEN does not carry, so an unreachable endpoint leaves the
// committed clones chip in place and says so rather than failing the refresh or
// publishing a wrong number.
func cloneCount() int {
	var clones struct {
		Count int `json:"count"`
	}
	if err := get("https://api.github.com/repos/"+repo+"/traffic/clones", &clones); err != nil {
		fmt.Fprintf(os.Stderr, "badges: clone traffic unavailable (%v); leaving that chip untouched, set BADGE_TOKEN to refresh it\n", err)
		return 0
	}
	return clones.Count
}

// compact renders large counts as 1.9k rather than 1918, which keeps a chip
// from growing a digit at a time.
func compact(n int) string {
	switch {
	case n >= 100000:
		return fmt.Sprintf("%dk", n/1000)
	case n >= 1000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	default:
		return fmt.Sprint(n)
	}
}

func get(url string, into any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	token := os.Getenv("BADGE_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
