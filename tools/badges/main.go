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
	stats, complete, err := collect()
	if err != nil {
		return err
	}
	// The card is one file, so a stat we could not measure would drop its whole
	// column on the next commit. Leaving the committed card alone keeps the
	// README honest and makes the missing token visible on the run summary.
	if !complete {
		fmt.Println("::warning::clone traffic was unavailable, so the stat card was left as committed; add a BADGE_TOKEN secret to refresh it")
		return fillTrendshift(trendshiftBadge())
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
	if err := write("stats-dark.svg", badge.Card(stats, badge.Dark)); err != nil {
		return err
	}
	return fillTrendshift(trendshiftBadge())
}

func write(name, svg string) error {
	return os.WriteFile(filepath.Join(outDir, name), []byte(svg+"\n"), 0o644)
}

func collect() ([]badge.Stat, bool, error) {
	var repoInfo struct {
		Stars   int `json:"stargazers_count"`
		License struct {
			SPDX string `json:"spdx_id"`
		} `json:"license"`
	}
	if err := get("https://api.github.com/repos/"+repo, &repoInfo); err != nil {
		return nil, false, err
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := get("https://api.github.com/repos/"+repo+"/releases?per_page=1", &releases); err != nil {
		return nil, false, err
	}
	latest := ""
	if len(releases) > 0 {
		latest = releases[0].TagName
	}

	clones, measured := cloneCount()

	license := repoInfo.License.SPDX
	if license == "" {
		license = "MIT"
	}

	stats := []badge.Stat{{Value: compact(repoInfo.Stars), Label: "stars", Color: amber}}
	if measured {
		stats = append(stats, badge.Stat{Value: compact(clones), Label: "clones · 14d", Color: purple})
	}
	return append(stats,
		badge.Stat{Value: latest, Label: "release", Color: blue},
		badge.Stat{Value: license, Label: "license", Color: subtle},
	), measured, nil
}

// cloneCount reads 14-day clone traffic. The endpoint needs push access, which
// the Actions GITHUB_TOKEN does not carry, so the caller is told whether the
// figure was actually measured rather than being handed a zero to publish.
func cloneCount() (int, bool) {
	var clones struct {
		Count int `json:"count"`
	}
	if err := get("https://api.github.com/repos/"+repo+"/traffic/clones", &clones); err != nil {
		fmt.Fprintf(os.Stderr, "badges: clone traffic unavailable: %v\n", err)
		return 0, false
	}
	return clones.Count, true
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

// trendshiftBadge is the banner Trendshift mints once a repository reaches
// GitHub Trending. Ours has a profile but no badge yet: the endpoint answers
// 500 until the day it trends, so the region in the README stays empty and
// fills itself on the first run after that happens.
func trendshiftBadge() string {
	const id = "89312"
	resp, err := http.Get("https://trendshift.io/api/badge/repositories/" + id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "badges: trendshift unreachable:", err)
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	return fmt.Sprintf(
		`<a href="https://trendshift.io/repositories/%s"><img src="https://trendshift.io/api/badge/repositories/%s" alt="agent-manager on Trendshift" width="250" height="55"></a>`,
		id, id)
}

// fillTrendshift keeps the README's trendshift region in step with whether the
// badge exists, without touching a byte outside the markers.
func fillTrendshift(badge string) error {
	const (
		open  = "<!-- trendshift:start -->"
		close = "<!-- trendshift:end -->"
	)
	raw, err := os.ReadFile("README.md")
	if err != nil {
		return err
	}
	readme := string(raw)
	from := strings.Index(readme, open)
	to := strings.Index(readme, close)
	if from < 0 || to < 0 {
		return fmt.Errorf("README is missing the trendshift markers")
	}
	updated := readme[:from+len(open)] + badge + readme[to:]
	if updated == readme {
		return nil
	}
	if badge == "" {
		fmt.Println("::notice::trendshift badge is not minted yet, leaving its region empty")
	} else {
		fmt.Println("::notice::trendshift badge is live, added to the README")
	}
	return os.WriteFile("README.md", []byte(updated), 0o644)
}
