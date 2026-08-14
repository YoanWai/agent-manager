// Command badges refreshes the repository's generated badge assets.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	repo     = "YoanWai/agent-manager"
	outDir   = "docs/badges"
	cloneInk = "6a4a94"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "badges:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeCloneEndpoint(); err != nil {
		return err
	}
	return refreshContributors()
}

// writeCloneEndpoint writes nothing when the traffic call fails, so the
// badges branch keeps its published figure and a missing token shows up on
// the run summary rather than as a zero in the README.
func writeCloneEndpoint() error {
	count, measured := cloneCount()
	if !measured {
		fmt.Println("::warning::clone traffic was unavailable, so the published count was left alone; add a BADGE_TOKEN secret to refresh it")
		return nil
	}
	endpoint := struct {
		SchemaVersion int    `json:"schemaVersion"`
		Label         string `json:"label"`
		Message       string `json:"message"`
		Color         string `json:"color"`
	}{1, "clones · 14d", compact(count), cloneInk}
	body, err := json.MarshalIndent(endpoint, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "clones.json"), append(body, '\n'), 0o644)
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
