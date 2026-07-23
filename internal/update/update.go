// Package update checks GitHub Releases for a newer agent-manager and
// caches the result so the TUI can show an unobtrusive "vX available"
// badge. It never self-replaces the binary: Homebrew and go install own
// the actual upgrade, so the badge just points users at them.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	releasesURL   = "https://api.github.com/repos/YoanWai/agent-manager/releases/latest"
	cacheFile     = "update-check.json"
	checkInterval = 24 * time.Hour
	requestBudget = 4 * time.Second
)

// Result is the newest release found. Latest is empty when the current
// build is already up to date (or the version is a dev build).
type Result struct {
	Latest string `json:"latest"`
	URL    string `json:"url"`
}

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
}

// Check returns the newest release when it is strictly newer than current.
// It throttles to one network call per checkInterval by caching in configDir,
// and returns an empty Result (no error) for dev builds or when up to date.
// Network and parse failures are returned so the caller can log them, but the
// TUI treats any error as "no badge" and moves on.
func Check(ctx context.Context, configDir, current string) (Result, error) {
	currentParts, ok := parseVersion(current)
	if !ok {
		return Result{}, nil
	}

	cachePath := filepath.Join(configDir, cacheFile)
	if cached, ok := readCache(cachePath); ok && time.Since(cached.CheckedAt) < checkInterval {
		return newerThan(currentParts, cached.Latest, cached.URL), nil
	}

	latest, url, err := fetchLatest(ctx)
	if err != nil {
		return Result{}, err
	}
	writeCache(cachePath, cache{CheckedAt: time.Now(), Latest: latest, URL: url})
	return newerThan(currentParts, latest, url), nil
}

func newerThan(current [3]int, latest, url string) Result {
	latestParts, ok := parseVersion(latest)
	if !ok || !greater(latestParts, current) {
		return Result{}
	}
	return Result{Latest: latest, URL: url}
}

func fetchLatest(ctx context.Context) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("update: github returned %s", resp.Status)
	}
	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	return release.TagName, release.HTMLURL, nil
}

func readCache(path string) (cache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cache{}, false
	}
	var c cache
	if err := json.Unmarshal(raw, &c); err != nil {
		return cache{}, false
	}
	return c, true
}

func writeCache(path string, c cache) {
	raw, err := json.Marshal(c)
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o644)
}

// parseVersion turns "v0.8.2" or "0.8.2" into its three numeric parts.
// A dev build or any tag without three numeric components returns ok=false
// so it is treated as un-comparable and never triggers a badge.
func parseVersion(v string) ([3]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	if idx := strings.IndexAny(v, "-+"); idx >= 0 {
		v = v[:idx]
	}
	fields := strings.Split(v, ".")
	if len(fields) != 3 {
		return [3]int{}, false
	}
	var parts [3]int
	for i, field := range fields {
		n, err := strconv.Atoi(field)
		if err != nil {
			return [3]int{}, false
		}
		parts[i] = n
	}
	return parts, true
}

func greater(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
