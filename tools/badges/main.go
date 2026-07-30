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
	green  = "#85b26f"
	purple = "#a78bd0"
	red    = "#cc6a6a"
	subtle = "#7d8590"
)

// lightInk darkens the dark-theme accents enough to clear 4.5:1 against a
// white surface, so the same chip reads in either GitHub theme.
var lightInk = map[string]string{
	amber:  "#96591f",
	blue:   "#2f5f8f",
	green:  "#4a7336",
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
	chips, err := collect()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	for name, chip := range chips {
		light := chip
		light.Color = lightInk[chip.Color]
		if err := write(name+"-light.svg", light.SVG(badge.Light)); err != nil {
			return err
		}
		if err := write(name+"-dark.svg", chip.SVG(badge.Dark)); err != nil {
			return err
		}
	}
	return nil
}

func write(name, svg string) error {
	return os.WriteFile(filepath.Join(outDir, name), []byte(svg+"\n"), 0o644)
}

func collect() (map[string]badge.Chip, error) {
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

	goVersion, err := goDirective()
	if err != nil {
		return nil, err
	}

	license := repoInfo.License.SPDX
	if license == "" {
		license = "MIT"
	}

	chips := map[string]badge.Chip{
		"stars":    {Label: "stars", Value: compact(repoInfo.Stars), Color: amber, Icon: "star"},
		"release":  {Label: "release", Value: latest, Color: blue, Icon: "tag"},
		"go":       {Label: "go", Value: goVersion, Color: green, Icon: ""},
		"license":  {Label: "license", Value: license, Color: subtle, Icon: "law"},
		"platform": {Label: "platform", Value: "macOS/Linux/WSL2", Color: subtle, Icon: ""},
	}
	if clones > 0 {
		chips["clones"] = badge.Chip{Label: "clones/14d", Value: compact(clones), Color: purple, Icon: "repo"}
	}
	return chips, nil
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

// goDirective reads the module's minimum toolchain out of go.mod, so the chip
// cannot drift from what the build actually requires.
func goDirective() (string, error) {
	raw, err := os.ReadFile("go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if version, ok := strings.CutPrefix(strings.TrimSpace(line), "go "); ok {
			parts := strings.Split(strings.TrimSpace(version), ".")
			if len(parts) >= 2 {
				return parts[0] + "." + parts[1] + "+", nil
			}
			return strings.TrimSpace(version), nil
		}
	}
	return "", fmt.Errorf("no go directive in go.mod")
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
