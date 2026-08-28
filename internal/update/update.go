// Package update discovers agent-manager releases and keeps a bounded local
// catalog of their user-facing changes. The TUI derives both update prompts
// and post-update summaries from that one catalog.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/charmbracelet/x/ansi"
)

const (
	cacheFile            = "update-check.json"
	checkInterval        = 10 * time.Minute
	requestBudget        = 4 * time.Second
	refreshBudget        = 2 * time.Minute
	maxPayload           = 16 << 20
	maxReleases          = 100
	maxChangesPerRelease = 12
	maxChangeLength      = 120
	maxHighlights        = 6
)

const (
	// highlightsHeading is the section the maintainer writes for this panel:
	// short bullets naming what the release gives someone. The generated
	// list says which branches merged, which is not the same question.
	highlightsHeading = "## Highlights"
	changesHeading    = "## What's Changed"
)

// releasesURL is a var so tests can point the fetch at a local server.
var releasesURL = "https://api.github.com/repos/YoanWai/agent-manager/releases?per_page=100"

// userFacingTypes are the conventional commit types that change what the
// program does for the person reading the digest. The rest are real work
// that leaves the running program identical, and every row one of them
// takes is a row a feature or a fix does not get.
var userFacingTypes = map[string]bool{"feat": true, "fix": true, "perf": true}

var (
	conventionalTitle = regexp.MustCompile(`(?i)^(feat|fix|docs|refactor|perf|test|build|ci|chore|style)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	pullSuffix        = regexp.MustCompile(`\s+by\s+(@[A-Za-z0-9-]+(?:\[bot])?)\s+in\s+https://github\.com/\S+\s*$`)
	markdownLink      = regexp.MustCompile(`\[([^]]+)]\([^)]+\)`)
)

// Release is one stable GitHub release with a compact, terminal-safe summary.
// TotalChanges may exceed len(Changes) when a large release was bounded.
type Release struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	// Highlights are the maintainer's own bullets for this release, empty
	// when the notes carry none. They stand in for Changes when present.
	Highlights   []string `json:"highlights,omitempty"`
	Changes      []string `json:"changes"`
	TotalChanges int      `json:"total_changes"`
}

// Result is the locally known release catalog plus an optional release newer
// than the running build. Releases are ordered newest first.
type Result struct {
	Latest   string
	URL      string
	Releases []Release
}

// ReleaseRange is the stable release history inside an exclusive/inclusive
// version range. Releases are ordered oldest first for reading. Complete is
// false when GitHub's bounded catalog did not reach one of the range edges.
type ReleaseRange struct {
	Releases []Release
	Complete bool
}

type cache struct {
	CheckedAt time.Time `json:"checked_at"`
	// Latest and URL keep the cache backward-readable by older binaries.
	Latest   string    `json:"latest"`
	URL      string    `json:"url"`
	ETag     string    `json:"etag,omitempty"`
	Releases []Release `json:"releases,omitempty"`
}

type githubRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

// Check returns a cache-backed release catalog. Every build checks at most once
// per ten minutes, using an ETag after the first response so unchanged checks
// transfer no release body.
func Check(ctx context.Context, configDir, current string) (Result, error) {
	return check(ctx, configDir, current, false)
}

// Refresh bypasses the cache age and asks GitHub for the current catalog.
func Refresh(ctx context.Context, configDir, current string) (Result, error) {
	return check(ctx, configDir, current, true)
}

// Cached returns a catalog already on disk without making a network request.
// It is used during startup so post-update summaries can paint immediately.
func Cached(configDir, current string) Result {
	currentParts, ok := parseVersion(current)
	if !ok {
		return Result{}
	}
	cached, ok := readCache(filepath.Join(configDir, cacheFile))
	if !ok || len(cached.Releases) == 0 {
		return Result{}
	}
	return resultFor(currentParts, cached.Releases)
}

func check(ctx context.Context, configDir, current string, force bool) (Result, error) {
	currentParts, ok := parseVersion(current)
	if !ok {
		return Result{}, nil
	}

	cachePath := filepath.Join(configDir, cacheFile)
	cached, haveCache := readCache(cachePath)
	haveCatalog := haveCache && len(cached.Releases) > 0
	newest, _ := latestRelease(cached.Releases)
	// A catalog that stops short of the running build was fetched before the
	// release that is now installed, so however recently that was, it cannot
	// name what the update brought. Updating within the interval lands in
	// exactly that state, which is when the notes are read.
	if !force && haveCatalog && cacheFresh(cached, time.Now()) && !Newer(current, newest) {
		return resultFor(currentParts, cached.Releases), nil
	}

	etag := ""
	if haveCatalog {
		etag = cached.ETag
	}
	// A background Check gives up quickly and silently; a Refresh answers
	// a user who is waiting on it, so it waits like the download does.
	budget := requestBudget
	if force {
		budget = refreshBudget
	}
	releases, nextETag, notModified, err := fetchReleases(ctx, etag, budget)
	if err != nil {
		if haveCatalog {
			return resultFor(currentParts, cached.Releases), err
		}
		return Result{}, err
	}
	if notModified {
		cached.CheckedAt = time.Now()
		writeCache(cachePath, cached)
		return resultFor(currentParts, cached.Releases), nil
	}
	latest, releaseURL := latestRelease(releases)
	writeCache(cachePath, cache{
		CheckedAt: time.Now(),
		Latest:    latest,
		URL:       releaseURL,
		ETag:      nextETag,
		Releases:  releases,
	})
	return resultFor(currentParts, releases), nil
}

func cacheFresh(c cache, now time.Time) bool {
	age := now.Sub(c.CheckedAt)
	return age >= 0 && age < checkInterval
}

func resultFor(current [3]int, releases []Release) Result {
	result := Result{Releases: releases}
	if len(releases) == 0 {
		return result
	}
	latestParts, ok := parseVersion(releases[0].Version)
	if ok && greater(latestParts, current) {
		result.Latest = releases[0].Version
		result.URL = releases[0].URL
	}
	return result
}

func latestRelease(releases []Release) (string, string) {
	if len(releases) == 0 {
		return "", ""
	}
	return releases[0].Version, releases[0].URL
}

func fetchReleases(ctx context.Context, etag string, budget time.Duration) ([]Release, string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return nil, "", false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && etag != "" {
		return nil, etag, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", false, fmt.Errorf("update: github returned %s", resp.Status)
	}

	var raw []githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPayload)).Decode(&raw); err != nil {
		return nil, "", false, err
	}

	releases := make([]Release, 0, min(len(raw), maxReleases))
	seen := make(map[string]bool, len(raw))
	for _, item := range raw {
		if len(releases) == maxReleases {
			break
		}
		if item.Draft || item.Prerelease || seen[item.TagName] {
			continue
		}
		if _, ok := parseVersion(item.TagName); !ok || !safeReleaseURL(item.HTMLURL) {
			continue
		}
		changes, total := extractChanges(item.Body)
		releases = append(releases, Release{
			Version:      item.TagName,
			URL:          item.HTMLURL,
			Highlights:   extractHighlights(item.Body),
			Changes:      changes,
			TotalChanges: total,
		})
		seen[item.TagName] = true
	}
	sort.Slice(releases, func(i, j int) bool {
		a, _ := parseVersion(releases[i].Version)
		b, _ := parseVersion(releases[j].Version)
		return greater(a, b)
	})
	return releases, resp.Header.Get("ETag"), false, nil
}

func safeReleaseURL(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host == "github.com"
}

// sectionLines returns the trimmed lines under a level-two heading. It
// stops at the next heading or at the generated changelog footer, so a
// section never bleeds into the install instructions below it.
func sectionLines(body, heading string) []string {
	var lines []string
	inside := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, heading) {
			inside = true
			continue
		}
		if !inside {
			continue
		}
		if strings.HasPrefix(trimmed, "## ") || strings.HasPrefix(trimmed, "**Full Changelog**") {
			break
		}
		lines = append(lines, trimmed)
	}
	return lines
}

// bulletText returns the text of a markdown bullet; any other line is not one.
func bulletText(line string) (string, bool) {
	if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
		return "", false
	}
	return strings.TrimSpace(line[2:]), true
}

// extractHighlights reads the authored bullets. Prose in that section is
// the release page's own copy, written for a browser rather than for a
// modal, so only bullets reach the panel.
func extractHighlights(body string) []string {
	var highlights []string
	for _, line := range sectionLines(body, highlightsHeading) {
		text, ok := bulletText(line)
		if !ok {
			continue
		}
		if text = plainText(text); text == "" {
			continue
		}
		highlights = append(highlights, sentenceCase(truncateChange(text)))
		if len(highlights) == maxHighlights {
			break
		}
	}
	return highlights
}

func extractChanges(body string) ([]string, int) {
	var changes []string
	total := 0
	for _, line := range sectionLines(body, changesHeading) {
		bullet, ok := bulletText(line)
		if !ok {
			continue
		}
		change, kind := cleanChange(bullet)
		if change == "" {
			continue
		}
		// A bullet that names no type cannot be judged, so it stays.
		if kind != "" && !userFacingTypes[kind] {
			continue
		}
		total++
		if len(changes) < maxChangesPerRelease {
			changes = append(changes, change)
		}
	}
	return changes, total
}

// cleanChange returns the digest row for a generated bullet and the
// conventional type it declared, empty when it declared none.
func cleanChange(change string) (string, string) {
	// Credit outside contributors on their digest lines; the maintainer's
	// own handle and bot handles would be noise on every row.
	author := ""
	if match := pullSuffix.FindStringSubmatch(change); match != nil {
		if handle := match[1]; handle != "@YoanWai" && !strings.HasSuffix(handle, "[bot]") {
			author = handle
		}
	}
	change = plainText(pullSuffix.ReplaceAllString(change, ""))
	kind := ""
	if match := conventionalTitle.FindStringSubmatch(change); match != nil {
		kind = strings.ToLower(match[1])
		description := sentenceCase(match[3])
		if scope := labelCase(match[2]); scope != "" {
			change = scope + ": " + description
		} else {
			change = description
		}
	} else {
		change = sentenceCase(change)
	}
	change = truncateChange(change)
	if author != "" {
		change += " · " + author
	}
	return change, kind
}

// plainText renders a markdown fragment as the text the panel paints: a
// link becomes its label, a code span loses its fences, and any control
// sequence the notes carried is dropped.
func plainText(text string) string {
	text = markdownLink.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "`", "")
	return cleanText(text)
}

func truncateChange(text string) string {
	runes := []rune(text)
	if len(runes) <= maxChangeLength {
		return text
	}
	return strings.TrimSpace(string(runes[:maxChangeLength-1])) + "…"
}

func cleanText(text string) string {
	var out strings.Builder
	for _, r := range ansi.Strip(text) {
		if unicode.IsControl(r) {
			continue
		}
		out.WriteRune(r)
	}
	return strings.TrimSpace(out.String())
}

func sentenceCase(text string) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
	}
	return string(runes)
}

func labelCase(label string) string {
	words := strings.Fields(strings.ReplaceAll(label, "-", " "))
	if len(words) == 0 {
		return ""
	}
	for i, word := range words {
		if releaseAcronym(word) {
			words[i] = strings.ToUpper(word)
		}
	}
	words[0] = sentenceCase(words[0])
	return strings.Join(words, " ")
}

func releaseAcronym(word string) bool {
	switch strings.ToLower(word) {
	case "ai", "api", "ci", "cli", "cpu", "mcp", "os", "pr", "ram", "tui", "ui":
		return true
	default:
		return false
	}
}

// Between selects releases newer than after and no newer than through.
func Between(releases []Release, after, through string) ReleaseRange {
	afterParts, afterOK := parseVersion(after)
	throughParts, throughOK := parseVersion(through)
	if !afterOK || !throughOK || greater(afterParts, throughParts) {
		return ReleaseRange{}
	}

	result := ReleaseRange{}
	lowerCovered := false
	upperCovered := false
	for i := len(releases) - 1; i >= 0; i-- {
		release := releases[i]
		parts, ok := parseVersion(release.Version)
		if !ok {
			continue
		}
		if !greater(parts, afterParts) {
			lowerCovered = true
		}
		if !greater(throughParts, parts) {
			upperCovered = true
		}
		if greater(parts, afterParts) && !greater(parts, throughParts) {
			result.Releases = append(result.Releases, release)
		}
	}
	result.Complete = lowerCovered && upperCovered
	return result
}

// Newer reports whether version is a stable semantic version newer than base.
func Newer(version, base string) bool {
	versionParts, versionOK := parseVersion(version)
	baseParts, baseOK := parseVersion(base)
	return versionOK && baseOK && greater(versionParts, baseParts)
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
	_ = atomicfile.WriteFile(path, raw, 0o644)
}

// parseVersion turns "v0.8.2" or "0.8.2" into its three numeric parts.
// A dev build or any tag without three numeric components returns ok=false.
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

// VersionWithin reports whether version falls inside the inclusive
// [minimum, maximum] range. An empty bound is open.
func VersionWithin(version, minimum, maximum string) bool {
	if minimum == "" && maximum == "" {
		return true
	}
	parts, ok := parseVersion(version)
	if !ok {
		return false
	}
	if minimum != "" {
		if low, ok := parseVersion(minimum); !ok || greater(low, parts) {
			return false
		}
	}
	if maximum != "" {
		if high, ok := parseVersion(maximum); !ok || greater(parts, high) {
			return false
		}
	}
	return true
}

func greater(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
