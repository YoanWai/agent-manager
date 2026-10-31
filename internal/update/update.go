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
	cacheFile       = "release-catalog.json"
	legacyCacheFile = "update-check.json"
	// catalogParser names the shape this build parses a release into. Raise it when that shape changes.
	catalogParser        = 2
	checkInterval        = 10 * time.Minute
	requestBudget        = 4 * time.Second
	refreshBudget        = 2 * time.Minute
	maxPayload           = 16 << 20
	maxReleases          = 100
	maxChangesPerRelease = 100
	maxLineLength        = 160
	maxHighlights        = 8
	maxThanks            = 24
	maxHeadlineLength    = 80
	maxSummaryLength     = 600
)

const (
	// highlightsHeading is the section the maintainer writes for this panel:
	// short bullets naming what the release gives someone. The generated
	// list says which branches merged, which is not the same question.
	highlightsHeading = "## Highlights"
	thanksHeading     = "## Thank you"
	changesHeading    = "## What's Changed"
	// labeledNotesSince is the first release whose highlights open with their label in backticks.
	labeledNotesSince = "v0.46.0"
)

// Thanks groups, as the Thank you section's sub-headings file them.
const (
	ThanksBuilt   = "built"
	ThanksFound   = "found"
	ThanksStarted = "started"
)

var thanksGroupHeadings = map[string]string{
	"### built it":          ThanksBuilt,
	"### found it":          ThanksFound,
	"### started something": ThanksStarted,
}

// releasesURL is a var so tests can point the fetch at a local server.
var releasesURL = "https://api.github.com/repos/YoanWai/agent-manager/releases?per_page=100"

var (
	conventionalTitle = regexp.MustCompile(`(?i)^(feat|fix|docs|refactor|perf|test|build|ci|chore|style)(?:\(([^)]+)\))?!?:\s*(.+)$`)
	pullSuffix        = regexp.MustCompile(`\s+by\s+(@[A-Za-z0-9-]+(?:\[bot])?)\s+in\s+https://github\.com/\S+\s*$`)
	markdownLink      = regexp.MustCompile(`\[([^]]+)]\([^)]+\)`)
	boldLine          = regexp.MustCompile(`^\*\*([^*]+)\*\*$`)
	highlightLabel    = regexp.MustCompile("^`([^`]+)`\\s*(.*)$")
	thanksRefs        = regexp.MustCompile(`\s*\((#\d+(?:,\s*#\d+)*)\)\.?$`)
)

const (
	KindFeature = "feat"
	KindFix     = "fix"
	KindOther   = "other"
)

// Highlight is one authored bullet. Label is empty for releases before labeledNotesSince.
type Highlight struct {
	Label string `json:"label,omitempty"`
	Text  string `json:"text"`
}

// Thanks is one contributor line. Handle is empty when the bullet does not open with one.
type Thanks struct {
	Handle string   `json:"handle,omitempty"`
	Group  string   `json:"group"`
	Text   string   `json:"text"`
	Refs   []string `json:"refs,omitempty"`
}

// UnmarshalJSON also reads the plain string a parser 1 catalog stored, so that catalog paints until it is refetched.
func (highlight *Highlight) UnmarshalJSON(raw []byte) error {
	if !isJSONString(raw) {
		type fields Highlight
		return json.Unmarshal(raw, (*fields)(highlight))
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return err
	}
	*highlight = Highlight{Text: text}
	return nil
}

// UnmarshalJSON also reads the plain string a parser 1 catalog stored, filed under Built it.
func (thanks *Thanks) UnmarshalJSON(raw []byte) error {
	if !isJSONString(raw) {
		type fields Thanks
		return json.Unmarshal(raw, (*fields)(thanks))
	}
	var line string
	if err := json.Unmarshal(raw, &line); err != nil {
		return err
	}
	*thanks = parseThanks(line, ThanksBuilt)
	return nil
}

func isJSONString(raw []byte) bool {
	return len(raw) > 0 && raw[0] == '"'
}

// Change is one line of a release's generated list.
type Change struct {
	Kind   string `json:"kind"`
	Scope  string `json:"scope,omitempty"`
	Text   string `json:"text"`
	Author string `json:"author,omitempty"`
}

// Release is one stable GitHub release with a compact, terminal-safe summary.
// Changes holds every generated line up to a bound that TotalChanges may exceed.
// Summary and highlight text keep their accent backticks. A highlight's Label is stored without them.
type Release struct {
	Version      string      `json:"version"`
	URL          string      `json:"url"`
	PublishedAt  time.Time   `json:"published_at,omitempty"`
	Headline     string      `json:"headline,omitempty"`
	Summary      string      `json:"summary,omitempty"`
	Highlights   []Highlight `json:"highlights,omitempty"`
	Thanks       []Thanks    `json:"thanks,omitempty"`
	Changes      []Change    `json:"changes,omitempty"`
	TotalChanges int         `json:"total_changes"`
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
	Parser    int       `json:"parser"`
	ETag      string    `json:"etag,omitempty"`
	Releases  []Release `json:"releases,omitempty"`
}

// legacyCache is update-check.json from releases before v0.40.0, whose change rows carry no kind and seed as other.
type legacyCache struct {
	Releases []struct {
		Version      string      `json:"version"`
		URL          string      `json:"url"`
		Highlights   []Highlight `json:"highlights"`
		Thanks       []Thanks    `json:"thanks"`
		Changes      []string    `json:"changes"`
		TotalChanges int         `json:"total_changes"`
	} `json:"releases"`
}

type githubRelease struct {
	TagName     string    `json:"tag_name"`
	HTMLURL     string    `json:"html_url"`
	Body        string    `json:"body"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
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
	cached, _ := loadCatalog(configDir)
	if len(cached.Releases) == 0 {
		return Result{}
	}
	return resultFor(currentParts, cached.Releases)
}

// loadCatalog reports parsedHere false for a catalog it must neither trust as fresh nor revalidate by ETag.
func loadCatalog(configDir string) (cached cache, parsedHere bool) {
	if stored, ok := atomicfile.ReadJSON[cache](filepath.Join(configDir, cacheFile)); ok {
		return stored, stored.Parser == catalogParser
	}
	return legacyCatalog(filepath.Join(configDir, legacyCacheFile)), false
}

func legacyCatalog(path string) cache {
	raw, err := os.ReadFile(path)
	if err != nil {
		return cache{}
	}
	var legacy legacyCache
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return cache{}
	}
	var seeded cache
	for _, release := range legacy.Releases {
		changes := make([]Change, 0, len(release.Changes))
		for _, row := range release.Changes {
			changes = append(changes, Change{Kind: KindOther, Text: row})
		}
		seeded.Releases = append(seeded.Releases, Release{
			Version:      release.Version,
			URL:          release.URL,
			Highlights:   release.Highlights,
			Thanks:       release.Thanks,
			Changes:      changes,
			TotalChanges: release.TotalChanges,
		})
	}
	return seeded
}

func check(ctx context.Context, configDir, current string, force bool) (Result, error) {
	currentParts, ok := parseVersion(current)
	if !ok {
		return Result{}, nil
	}

	cachePath := filepath.Join(configDir, cacheFile)
	cached, parsedHere := loadCatalog(configDir)
	haveCatalog := len(cached.Releases) > 0
	newest, _ := latestRelease(cached.Releases)
	// A catalog that stops short of the running build was fetched before the
	// release that is now installed, so however recently that was, it cannot
	// name what the update brought. Updating within the interval lands in
	// exactly that state, which is when the notes are read.
	if !force && parsedHere && haveCatalog && cacheFresh(cached, time.Now()) && !Newer(current, newest) {
		return resultFor(currentParts, cached.Releases), nil
	}

	etag := ""
	if parsedHere && haveCatalog {
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
		_ = atomicfile.WriteJSON(cachePath, cached, 0o644)
		return resultFor(currentParts, cached.Releases), nil
	}
	_ = atomicfile.WriteJSON(cachePath, cache{
		CheckedAt: time.Now(),
		Parser:    catalogParser,
		ETag:      nextETag,
		Releases:  releases,
	}, 0o644)
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
		headline, summary := extractLead(item.Body, item.TagName)
		releases = append(releases, Release{
			Version:      item.TagName,
			URL:          item.HTMLURL,
			PublishedAt:  item.PublishedAt,
			Headline:     headline,
			Summary:      summary,
			Highlights:   extractHighlights(item.Body, item.TagName),
			Thanks:       extractThanks(item.Body),
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

func bulletText(line string) (string, bool) {
	if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
		return "", false
	}
	return strings.TrimSpace(line[2:]), true
}

// extractHighlights keeps only bullets, since the prose under the heading is the release page's copy.
func extractHighlights(body, tag string) []Highlight {
	labeled := !Newer(labeledNotesSince, tag)
	var highlights []Highlight
	for _, line := range sectionLines(body, highlightsHeading) {
		bullet, ok := bulletText(line)
		if !ok {
			continue
		}
		text := markedText(bullet)
		if text == "" {
			continue
		}
		text = truncate(text, maxLineLength)
		if match := highlightLabel.FindStringSubmatch(text); labeled && match != nil {
			highlights = append(highlights, Highlight{Label: match[1], Text: sentenceCase(match[2])})
		} else {
			highlights = append(highlights, Highlight{Text: sentenceCase(text)})
		}
		if len(highlights) == maxHighlights {
			break
		}
	}
	return highlights
}

func extractThanks(body string) []Thanks {
	var thanks []Thanks
	group := ThanksBuilt
	for _, line := range sectionLines(body, thanksHeading) {
		if heading, ok := thanksGroupHeadings[strings.ToLower(line)]; ok {
			group = heading
			continue
		}
		bullet, ok := bulletText(line)
		if !ok {
			continue
		}
		entry := parseThanks(plainText(bullet), group)
		if entry.Handle == "" && entry.Text == "" {
			continue
		}
		thanks = append(thanks, entry)
		if len(thanks) == maxThanks {
			break
		}
	}
	return thanks
}

// parseThanks splits "@handle what they did (#1, #2)" and keeps the visible line within maxLineLength.
func parseThanks(line, group string) Thanks {
	entry := Thanks{Group: group}
	text := strings.TrimSpace(line)
	if strings.HasPrefix(text, "@") {
		handle, rest, _ := strings.Cut(text, " ")
		entry.Handle, text = handle, strings.TrimSpace(rest)
	}
	if match := thanksRefs.FindStringSubmatchIndex(text); match != nil {
		for _, ref := range strings.Split(text[match[2]:match[3]], ",") {
			entry.Refs = append(entry.Refs, strings.TrimSpace(ref))
		}
		text = text[:match[0]]
	}
	budget := maxLineLength - len([]rune(entry.Handle)) - 1
	if len(entry.Refs) > 0 {
		budget -= len([]rune(" (" + strings.Join(entry.Refs, ", ") + ")"))
	}
	entry.Text = truncate(text, max(budget, 1))
	return entry
}

// extractLead reads an optional bold headline, then the summary, under the release's own heading.
func extractLead(body, tag string) (headline, summary string) {
	var paragraph []string
	for _, line := range sectionLines(body, "## "+tag) {
		if line == "" {
			continue
		}
		if match := boldLine.FindStringSubmatch(line); match != nil && headline == "" && len(paragraph) == 0 {
			headline = truncate(plainText(match[1]), maxHeadlineLength)
			continue
		}
		paragraph = append(paragraph, line)
	}
	joined := strings.ReplaceAll(strings.Join(paragraph, " "), "**", "")
	return headline, truncate(markedText(joined), maxSummaryLength)
}

func extractChanges(body string) ([]Change, int) {
	var changes []Change
	total := 0
	for _, line := range sectionLines(body, changesHeading) {
		bullet, ok := bulletText(line)
		if !ok {
			continue
		}
		change, ok := cleanChange(bullet)
		if !ok {
			continue
		}
		total++
		if len(changes) < maxChangesPerRelease {
			changes = append(changes, change)
		}
	}
	return changes, total
}

func cleanChange(bullet string) (Change, bool) {
	change := Change{Kind: KindOther}
	// Credit outside contributors on their digest lines; the maintainer's
	// own handle and bot handles would be noise on every row.
	if match := pullSuffix.FindStringSubmatch(bullet); match != nil {
		if handle := match[1]; handle != "@YoanWai" && !strings.HasSuffix(handle, "[bot]") {
			change.Author = handle
		}
	}
	row := plainText(pullSuffix.ReplaceAllString(bullet, ""))
	if match := conventionalTitle.FindStringSubmatch(row); match != nil {
		switch strings.ToLower(match[1]) {
		case "feat":
			change.Kind = KindFeature
		case "fix", "perf":
			change.Kind = KindFix
		}
		change.Scope = labelCase(match[2])
		row = sentenceCase(match[3])
	} else {
		row = sentenceCase(row)
	}
	if row == "" {
		return Change{}, false
	}
	change.Text = truncate(row, maxLineLength)
	return change, true
}

func plainText(text string) string {
	text = markdownLink.ReplaceAllString(text, "$1")
	text = strings.ReplaceAll(text, "`", "")
	return cleanText(text)
}

// markedText keeps accent marks only when every span closes, since one stray backtick would accent the rest of the line.
func markedText(text string) string {
	text = cleanText(markdownLink.ReplaceAllString(text, "$1"))
	if strings.Count(text, "`")%2 != 0 {
		return strings.ReplaceAll(text, "`", "")
	}
	return text
}

// truncate counts visible characters, so accent marks are free and a span the cut leaves open is closed.
func truncate(text string, limit int) string {
	if len([]rune(text))-strings.Count(text, "`") <= limit {
		return text
	}
	var kept strings.Builder
	visible, open := 0, false
	for _, character := range text {
		if visible == limit-1 {
			break
		}
		kept.WriteRune(character)
		if character == '`' {
			open = !open
		} else {
			visible++
		}
	}
	cut := strings.TrimRight(kept.String(), " ")
	if open {
		cut += "`"
	}
	return cut + "…"
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
	case "ai", "api", "ci", "cli", "cpu", "e2e", "mcp", "os", "pr", "ram", "tui", "ui":
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
