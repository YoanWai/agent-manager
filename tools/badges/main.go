// Command badges refreshes the repository's generated badge assets.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// apiBase is a variable so tests can point the traffic and contributor
// calls at a local server.
var apiBase = "https://api.github.com"

const (
	repo     = "wainmann/agent-manager"
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
// badges branch keeps its published figure and the failure shows up on the
// run summary rather than as a zero in the README. The one exception is a
// BADGE_TOKEN that GitHub rejects with 401 or 403: that returns an error so
// the run fails instead of quietly keeping a stale count. Without
// BADGE_TOKEN the call falls back to the Actions GITHUB_TOKEN, which cannot
// read traffic, so a 403 there is expected and only warns.
func writeCloneEndpoint() error {
	count, err := cloneCount()
	if err != nil {
		badgeToken := os.Getenv("BADGE_TOKEN") != ""
		var status *statusError
		if badgeToken && errors.As(err, &status) && (status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden) {
			return fmt.Errorf("BADGE_TOKEN is set but GitHub rejected it for clone traffic (%s); renew the token or restore its access to %s", status.Status, repo)
		}
		fmt.Fprintf(os.Stderr, "badges: clone traffic unavailable: %v\n", err)
		if badgeToken {
			fmt.Println("::warning::clone traffic was unavailable, so the published count was left alone")
		} else {
			fmt.Println("::warning::clone traffic was unavailable, so the published count was left alone; add a BADGE_TOKEN secret to refresh it")
		}
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
// the Actions GITHUB_TOKEN does not carry, so a failed call comes back as an
// error rather than a zero to publish. A non-200 answer is a *statusError.
func cloneCount() (int, error) {
	var clones struct {
		Count int `json:"count"`
	}
	if err := get(apiBase+"/repos/"+repo+"/traffic/clones", &clones); err != nil {
		return 0, err
	}
	return clones.Count, nil
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

// statusError is a GitHub API answer other than 200 OK, kept typed so callers
// can tell a rejected token from a network failure.
type statusError struct {
	URL    string
	Code   int
	Status string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("GET %s: %s", e.URL, e.Status)
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
		return &statusError{URL: url, Code: resp.StatusCode, Status: resp.Status}
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
