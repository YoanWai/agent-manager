package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
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
	for _, test := range cases {
		got, ok := parseVersion(test.in)
		if ok != test.valid || got != test.want {
			t.Errorf("parseVersion(%q) = %v,%v want %v,%v", test.in, got, ok, test.want, test.valid)
		}
	}
}

func TestVersionComparison(t *testing.T) {
	if !Newer("v0.9.0", "v0.8.2") {
		t.Fatal("newer patch should compare greater")
	}
	if Newer("v0.8.2", "v0.8.2") || Newer("dev", "v0.8.2") {
		t.Fatal("equal and unparseable versions must not compare newer")
	}
	if !VersionWithin("v0.8.2", "v0.8.0", "v0.9.0") {
		t.Fatal("version should fall inside inclusive bounds")
	}
	if VersionWithin("v0.8.2", "v0.8.3", "") {
		t.Fatal("version below minimum should not match")
	}
	if VersionWithin("dev", "v0.8.0", "") {
		t.Fatal("dev builds should not match targeted ranges")
	}
}

func TestExtractChangesHumanizesGeneratedNotes(t *testing.T) {
	body := strings.Join([]string{
		"Project introduction",
		"",
		"## What's Changed",
		"* fix(groups): make group creation immediate and reliable by @YoanWai in https://github.com/YoanWai/agent-manager/pull/191",
		"* feat(config): add Pi as a built-in tool by @steveprentice in https://github.com/YoanWai/agent-manager/pull/201",
		"* chore(deps): bump gopsutil by @dependabot[bot] in https://github.com/YoanWai/agent-manager/pull/210",
		"* docs(readme): add a badge by @YoanWai in https://github.com/YoanWai/agent-manager/pull/211",
		"- feat(ui): add a [message browser](https://example.com) with `scrolling`",
		"- feat(mcp-editor): expose tool capabilities",
		"",
		"**Full Changelog**: https://github.com/example",
		"* fix(hidden): do not include this",
	}, "\n")

	changes, total := extractChanges(body)
	want := []Change{
		{Kind: KindFix, Text: "Groups: Make group creation immediate and reliable"},
		{Kind: KindFeature, Text: "Config: Add Pi as a built-in tool", Author: "@steveprentice"},
		{Kind: KindFeature, Text: "UI: Add a message browser with scrolling"},
		{Kind: KindFeature, Text: "MCP editor: Expose tool capabilities"},
	}
	if total != len(want) || !slices.Equal(changes, want) {
		t.Fatalf("extractChanges() = %+v total %d, want %+v", changes, total, want)
	}
}

func TestExtractChangesIsBoundedAndTerminalSafe(t *testing.T) {
	lines := []string{"## What's Changed"}
	for i := 0; i < maxChangesPerRelease+3; i++ {
		lines = append(lines, fmt.Sprintf("* fix(ui): item %02d \x1b[31mred\x1b[0m", i))
	}
	changes, total := extractChanges(strings.Join(lines, "\n"))
	if len(changes) != maxChangesPerRelease || total != maxChangesPerRelease+3 {
		t.Fatalf("got %d stored / %d total", len(changes), total)
	}
	if strings.Contains(changes[0].Text, "\x1b") {
		t.Fatalf("terminal control sequence survived: %q", changes[0].Text)
	}
}

func TestBetweenReturnsEverySkippedReleaseOldestFirst(t *testing.T) {
	catalog := []Release{
		testRelease("v0.5.0", "five"),
		testRelease("v0.4.0", "four"),
		testRelease("v0.3.0", "three"),
		testRelease("v0.2.0", "two"),
	}
	rangeResult := Between(catalog, "v0.2.0", "v0.5.0")
	if !rangeResult.Complete {
		t.Fatal("catalog containing both edges should be complete")
	}
	got := releaseVersions(rangeResult.Releases)
	want := []string{"v0.3.0", "v0.4.0", "v0.5.0"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("Between() = %v, want %v", got, want)
	}

	incomplete := Between(catalog[:2], "v0.1.0", "v0.5.0")
	if incomplete.Complete {
		t.Fatal("a catalog that does not reach the starting version must say so")
	}
}

func TestCheckFetchesStableCatalogAndFindsLatest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[
			{"tag_name":"v0.9.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.9.0","body":"## What's Changed\n* fix(ui): newest","draft":false,"prerelease":false},
			{"tag_name":"v0.11.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.11.0","body":"## What's Changed\n* feat(ui): actual latest","draft":false,"prerelease":false},
			{"tag_name":"v0.12.0-rc.1","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.12.0-rc.1","body":"","draft":false,"prerelease":true},
			{"tag_name":"v99.0.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v99.0.0","body":"","draft":true,"prerelease":false},
			{"tag_name":"garbage","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/garbage","body":"","draft":false,"prerelease":false}
		]`)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), t.TempDir(), "v0.8.0")
	if err != nil {
		t.Fatal(err)
	}
	if result.Latest != "v0.11.0" || len(result.Releases) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := result.Releases[0].Changes; len(got) != 1 || got[0] != (Change{Kind: KindFeature, Text: "UI: Actual latest"}) {
		t.Fatalf("release changes = %+v", got)
	}
}

func TestCheckDevBuildSkips(t *testing.T) {
	if result, err := Check(context.Background(), t.TempDir(), "dev"); err != nil || len(result.Releases) != 0 {
		t.Fatalf("dev build should skip: %+v, %v", result, err)
	}
}

func TestCheckUsesFreshCatalogWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{
		CheckedAt: time.Now(),
		Releases: []Release{
			testRelease("v0.9.0", "new"),
			testRelease("v0.8.2", "current"),
		},
	})

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || result.Latest != "v0.9.0" || calls.Load() != 0 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

// Updating inside the check interval leaves a catalog fetched before the
// release now running, and the what's-new notice reads that catalog for the
// changes the update brought. Freshness alone must not serve it.
func TestCatalogBehindTheRunningBuildRefetches(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{
		CheckedAt: time.Now(),
		Releases:  []Release{testRelease("v0.28.0", "the release before the one running")},
	})

	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.29.0")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.29.0")
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want one fetch", err, calls.Load())
	}
	if got := releaseVersions(result.Releases); len(got) == 0 || got[0] != "v0.29.0" {
		t.Fatalf("catalog is %v, want it to reach the running build", got)
	}
	// The notice is built from these lines, so reaching the release is only
	// half of it: an entry with no changes leaves it as empty as before.
	if got := result.Releases[0].Changes; len(got) != 1 || got[0].Text != "UI: Refreshed" {
		t.Fatalf("changes are %+v, want the fetched release's own", got)
	}
	if result.Latest != "" {
		t.Fatalf("nothing is newer than the running build, got %q", result.Latest)
	}
}

func TestUpToDateCatalogRetriesAfterTenMinutes(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{
		CheckedAt: time.Now().Add(-checkInterval - time.Minute),
		Releases:  []Release{testRelease("v0.8.2", "old latest")},
	})

	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.8.3")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || calls.Load() != 1 || result.Latest != "v0.8.3" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestKnownUpdateAlsoRetriesAfterTenMinutes(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{
		CheckedAt: time.Now().Add(-checkInterval - time.Minute),
		Releases: []Release{
			testRelease("v0.9.0", "new"),
			testRelease("v0.8.2", "current"),
		},
	})

	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.10.0")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || calls.Load() != 1 || result.Latest != "v0.10.0" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestRefreshBypassesFreshCatalog(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{CheckedAt: time.Now(), Releases: []Release{testRelease("v0.8.2", "current")}})
	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.9.0")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Refresh(context.Background(), dir, "v0.8.2")
	if err != nil || calls.Load() != 1 || result.Latest != "v0.9.0" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
}

func TestStaleCatalogUsesConditionalRequest(t *testing.T) {
	dir := t.TempDir()
	oldCheckedAt := time.Now().Add(-checkInterval - time.Minute)
	seedCache(t, dir, cache{
		CheckedAt: oldCheckedAt,
		ETag:      `"catalog-1"`,
		Releases:  []Release{testRelease("v0.8.2", "current")},
	})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("If-None-Match"); got != `"catalog-1"` {
			t.Errorf("If-None-Match = %q", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || calls.Load() != 1 || len(result.Releases) != 1 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
	}
	written, ok := readCache(filepath.Join(dir, cacheFile))
	if !ok || !written.CheckedAt.After(oldCheckedAt) || written.ETag != `"catalog-1"` || written.Parser != catalogParser {
		t.Fatalf("conditional refresh did not advance cache: %+v", written)
	}
}

func TestFetchFailureReturnsStaleCatalogWithoutOverwritingIt(t *testing.T) {
	dir := t.TempDir()
	seed := cache{
		CheckedAt: time.Now().Add(-checkInterval - time.Minute),
		Releases: []Release{
			testRelease("v0.9.0", "new"),
			testRelease("v0.8.2", "current"),
		},
	}
	seedCache(t, dir, seed)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err == nil || result.Latest != "v0.9.0" {
		t.Fatalf("stale result should survive: %+v, %v", result, err)
	}
	kept, ok := readCache(filepath.Join(dir, cacheFile))
	if !ok || !kept.CheckedAt.Equal(seed.CheckedAt) {
		t.Fatalf("failed fetch overwrote stale cache: %+v", kept)
	}
}

func TestCatalogIsWrittenToItsOwnFile(t *testing.T) {
	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.40.0")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	dir := t.TempDir()
	if _, err := Check(context.Background(), dir, "v0.39.0"); err != nil {
		t.Fatal(err)
	}
	written, ok := readCache(filepath.Join(dir, "release-catalog.json"))
	if !ok || written.Parser != catalogParser || len(written.Releases) != 1 {
		t.Fatalf("catalog not written with its parser: %+v", written)
	}
	if _, err := os.Stat(filepath.Join(dir, "update-check.json")); !os.IsNotExist(err) {
		t.Fatalf("the file older builds own must be left alone: %v", err)
	}
}

func TestCatalogFromAnotherParserIsRefetchedWithoutItsETag(t *testing.T) {
	dir := t.TempDir()
	stale, err := json.Marshal(cache{
		CheckedAt: time.Now(),
		Parser:    catalogParser + 1,
		ETag:      `"other-parser"`,
		Releases:  []Release{testRelease("v0.40.0", "kept for the first paint")},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedFile(t, dir, cacheFile, string(stale))
	if got := Cached(dir, "v0.39.0"); got.Latest != "v0.40.0" {
		t.Fatalf("another parser's releases still paint: %+v", got)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("If-None-Match = %q, want none: a not-modified answer would keep the other parse", got)
		}
		fmt.Fprint(w, `[{"tag_name":"v0.40.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.40.0","body":"## What's Changed\n* fix(ui): reparsed"}]`)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.39.0")
	if err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want one full fetch despite the fresh timestamp", err, calls.Load())
	}
	if got := result.Releases[0].Changes[0].Text; got != "UI: Reparsed" {
		t.Fatalf("changes = %q, want this build's parse", got)
	}
}

func TestLegacyCatalogSeedsTheFirstPaint(t *testing.T) {
	dir := t.TempDir()
	seedFile(t, dir, "update-check.json", legacyCatalogJSON)

	seeded := Cached(dir, "v0.39.0")
	if seeded.Latest != "v0.40.0" || len(seeded.Releases) != 2 {
		t.Fatalf("seed = %+v", seeded)
	}
	newest := seeded.Releases[0]
	if !slices.Equal(newest.Highlights, []string{"Pickers are here"}) || !slices.Equal(newest.Thanks, []string{"@someone asked (#1)"}) {
		t.Fatalf("the authored sections must carry over: %+v", newest)
	}
	if !slices.Equal(newest.Changes, []Change{{Kind: KindOther, Text: "UI: A feature · @someone"}}) || newest.TotalChanges != 1 {
		t.Fatalf("legacy change rows must seed as other: %+v", newest)
	}
	if older := seeded.Releases[1]; !slices.Equal(older.Changes, []Change{{Kind: KindOther, Text: "UI: Older"}}) {
		t.Fatalf("a release without highlights must not seed empty: %+v", older)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("If-None-Match = %q, want none", got)
		}
		fmt.Fprint(w, `[{"tag_name":"v0.40.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.40.0","body":"## What's Changed\n* feat(ui): fetched"}]`)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	if _, err := Check(context.Background(), dir, "v0.39.0"); err != nil || calls.Load() != 1 {
		t.Fatalf("err=%v calls=%d, want a fetch at once: a seed is never fresh", err, calls.Load())
	}
	kept, err := os.ReadFile(filepath.Join(dir, "update-check.json"))
	if err != nil || string(kept) != legacyCatalogJSON {
		t.Fatalf("the legacy file must stay byte for byte: %v", err)
	}
}

func TestFailedFetchKeepsTheLegacySeedOnScreen(t *testing.T) {
	dir := t.TempDir()
	seedFile(t, dir, "update-check.json", legacyCatalogJSON)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.39.0")
	if err == nil || result.Latest != "v0.40.0" {
		t.Fatalf("the seed should survive a failed fetch: %+v, %v", result, err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, cacheFile)); !os.IsNotExist(statErr) {
		t.Fatalf("a failed fetch must not write a catalog: %v", statErr)
	}
}

func testRelease(version string, changes ...string) Release {
	release := Release{
		Version:      version,
		URL:          "https://github.com/YoanWai/agent-manager/releases/tag/" + version,
		TotalChanges: len(changes),
	}
	for _, text := range changes {
		release.Changes = append(release.Changes, Change{Kind: KindOther, Text: text})
	}
	return release
}

func TestLongChangeIsCutAtTheLineLimit(t *testing.T) {
	body := "## What's Changed\n* fix(ui): " + strings.Repeat("a", maxLineLength+20)
	changes, _ := extractChanges(body)
	if got := len([]rune(changes[0].Text)); got != maxLineLength {
		t.Fatalf("change is %d characters, want %d", got, maxLineLength)
	}
	if !strings.HasSuffix(changes[0].Text, "…") {
		t.Fatalf("a cut change must say so: %q", changes[0].Text)
	}
}

func releaseVersions(releases []Release) []string {
	versions := make([]string, len(releases))
	for i, release := range releases {
		versions[i] = release.Version
	}
	return versions
}

func seedCache(t *testing.T, dir string, value cache) {
	t.Helper()
	value.Parser = catalogParser
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	seedFile(t, dir, cacheFile, string(raw))
}

func seedFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const legacyCatalogJSON = `{
	"checked_at": "2099-01-01T00:00:00Z",
	"latest": "v0.40.0",
	"url": "https://github.com/YoanWai/agent-manager/releases/tag/v0.40.0",
	"etag": "\"legacy-1\"",
	"releases": [
		{"version": "v0.40.0", "url": "https://github.com/YoanWai/agent-manager/releases/tag/v0.40.0",
		 "highlights": ["Pickers are here"], "thanks": ["@someone asked (#1)"],
		 "changes": ["UI: A feature · @someone"], "total_changes": 1},
		{"version": "v0.39.0", "url": "https://github.com/YoanWai/agent-manager/releases/tag/v0.39.0",
		 "changes": ["UI: Older"], "total_changes": 1}
	]
}`

func releaseServer(t *testing.T, calls *atomic.Int32, version string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprintf(w, `[{"tag_name":%q,"html_url":%q,"body":"## What's Changed\n* fix(ui): refreshed"}]`,
			version,
			"https://github.com/YoanWai/agent-manager/releases/tag/"+version,
		)
	}))
}

func swapReleasesURL(url string) func() {
	previous := releasesURL
	releasesURL = url
	return func() { releasesURL = previous }
}

func TestExtractChangesKeepsWhatAReaderCanAct(t *testing.T) {
	body := strings.Join([]string{
		"## What's Changed",
		"* feat(ui): a feature",
		"* fix(ui): a fix",
		"* perf(ui): a speedup",
		"* docs(readme): a badge",
		"* chore(deps): a bump",
		"* ci: a workflow tweak",
		"* build: a build tweak",
		"* style: a reformat",
		"* test: a case",
		"* refactor(ui): a rename",
		"* Ship the two agent skills for install via skills.sh",
	}, "\n")

	changes, total := extractChanges(body)
	want := []Change{
		{Kind: KindFeature, Text: "UI: A feature"},
		{Kind: KindFix, Text: "UI: A fix"},
		{Kind: KindFeature, Text: "UI: A speedup"},
		{Kind: KindOther, Text: "Ship the two agent skills for install via skills.sh"},
	}
	if total != len(want) || !slices.Equal(changes, want) {
		t.Fatalf("extractChanges() = %+v total %d, want %+v", changes, total, want)
	}
}

func TestExtractHighlightsReadsTheAuthoredBullets(t *testing.T) {
	body := strings.Join([]string{
		"A one line pitch nobody put a heading on.",
		"",
		"## Highlights",
		"",
		"The panel takes bullets, so this paragraph stays on the web page.",
		"",
		"- the session list can take the whole terminal",
		"* rows carry the agent's `last message` beside the name",
		"- [Full notes](https://example.com/notes) explain the rest",
		"",
		"## What's Changed",
		"* feat(ui): full screen sessions mode",
		"",
		"**Full Changelog**: https://example.com/compare",
	}, "\n")

	want := []string{
		"The session list can take the whole terminal",
		"Rows carry the agent's `last message` beside the name",
		"Full notes explain the rest",
	}
	if got := extractHighlights(body); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("extractHighlights() = %q, want %q", got, want)
	}
}

func TestExtractHighlightsIsEmptyWithoutTheSection(t *testing.T) {
	body := "## What's Changed\n* feat(ui): a feature\n"
	if got := extractHighlights(body); len(got) != 0 {
		t.Fatalf("extractHighlights() = %q, want none", got)
	}
}

func TestExtractHighlightsStopsAtTheCap(t *testing.T) {
	lines := []string{"## Highlights"}
	for i := 0; i < maxHighlights+3; i++ {
		lines = append(lines, fmt.Sprintf("- highlight %d", i))
	}
	got := extractHighlights(strings.Join(lines, "\n"))
	if len(got) != maxHighlights {
		t.Fatalf("extractHighlights() kept %d, want %d", len(got), maxHighlights)
	}
	if got[maxHighlights-1] != fmt.Sprintf("Highlight %d", maxHighlights-1) {
		t.Fatalf("extractHighlights() last = %q", got[maxHighlights-1])
	}
}

func TestExtractThanksReadsTheAuthoredBullets(t *testing.T) {
	body := strings.Join([]string{
		"## Highlights",
		"- the session list can take the whole terminal",
		"",
		"## Thank you",
		"",
		"A closing paragraph stays on the web page.",
		"",
		"- @dolutech asked for reboot recovery in #388 and then built the picker (#400)",
		"* @pandysp asked to hide the stats foot and the header in #408 and #409",
		"- [@fruch](https://github.com/fruch) reported that a live rename moved the worktree (#418)",
		"",
		"## What's Changed",
		"* feat(config): revive without an id opens the tool's session picker",
	}, "\n")

	want := []string{
		"@dolutech asked for reboot recovery in #388 and then built the picker (#400)",
		"@pandysp asked to hide the stats foot and the header in #408 and #409",
		"@fruch reported that a live rename moved the worktree (#418)",
	}
	if got := extractThanks(body); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("extractThanks() = %q, want %q", got, want)
	}
	if got := extractHighlights(body); len(got) != 1 {
		t.Fatalf("highlights leaked thanks: %q", got)
	}
}

func TestExtractThanksIsEmptyWithoutTheSection(t *testing.T) {
	body := "## Highlights\n- a feature\n\n## What's Changed\n* feat(ui): a feature\n"
	if got := extractThanks(body); len(got) != 0 {
		t.Fatalf("extractThanks() = %q, want none", got)
	}
}

func TestExtractThanksStopsAtTheCap(t *testing.T) {
	lines := []string{"## Thank you"}
	for i := 0; i < maxThanks+3; i++ {
		lines = append(lines, fmt.Sprintf("- @someone%d filed #%d", i, i))
	}
	got := extractThanks(strings.Join(lines, "\n"))
	if len(got) != maxThanks {
		t.Fatalf("extractThanks() kept %d, want %d", len(got), maxThanks)
	}
}

func TestCatalogCarriesHighlightsThroughTheCache(t *testing.T) {
	body := `## Highlights\n- the list can take the whole terminal\n\n## Thank you\n- @pandysp asked for full screen in #357\n\n## What's Changed\n* feat(ui): full screen sessions mode`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[{"tag_name":"v0.34.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.34.0","body":"%s","draft":false,"prerelease":false}]`, body)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	dir := t.TempDir()
	wantHighlights := []string{"The list can take the whole terminal"}
	wantThanks := []string{"@pandysp asked for full screen in #357"}
	fetched, err := Check(context.Background(), dir, "v0.33.0")
	if err != nil {
		t.Fatal(err)
	}
	if got := fetched.Releases[0].Highlights; fmt.Sprint(got) != fmt.Sprint(wantHighlights) {
		t.Fatalf("fetched highlights = %q, want %q", got, wantHighlights)
	}
	if got := fetched.Releases[0].Thanks; fmt.Sprint(got) != fmt.Sprint(wantThanks) {
		t.Fatalf("fetched thanks = %q, want %q", got, wantThanks)
	}
	cached := Cached(dir, "v0.33.0")
	if got := cached.Releases[0].Highlights; fmt.Sprint(got) != fmt.Sprint(wantHighlights) {
		t.Fatalf("cached highlights = %q, want %q", got, wantHighlights)
	}
	if got := cached.Releases[0].Thanks; fmt.Sprint(got) != fmt.Sprint(wantThanks) {
		t.Fatalf("cached thanks = %q, want %q", got, wantThanks)
	}
}

func TestExtractLeadReadsTheHeadlineAndTheSummary(t *testing.T) {
	body := strings.Join([]string{
		"A pitch the release tool writes above everything.",
		"",
		"## v0.40.0",
		"**Model + reasoning pickers are here!**",
		"",
		"Every session runs on its own model. Press `ctrl+x` for effort,",
		"read live from [each CLI](https://example.com).",
		"",
		"## Highlights",
		"- a highlight",
	}, "\n")

	headline, summary := extractLead(body, "v0.40.0")
	if headline != "Model + reasoning pickers are here!" {
		t.Fatalf("headline = %q", headline)
	}
	want := "Every session runs on its own model. Press `ctrl+x` for effort, read live from each CLI."
	if summary != want {
		t.Fatalf("summary = %q, want %q", summary, want)
	}
}

func TestExtractLeadWithoutABoldFirstLineHasNoHeadline(t *testing.T) {
	body := "## v0.39.0\n\nOne click now focuses a session, and **every** row opens its actions.\n\n## Highlights\n- a highlight"
	headline, summary := extractLead(body, "v0.39.0")
	if headline != "" {
		t.Fatalf("a paragraph is not a headline: %q", headline)
	}
	if summary != "One click now focuses a session, and every row opens its actions." {
		t.Fatalf("summary = %q", summary)
	}
}

func TestExtractLeadIsEmptyWithoutTheVersionSection(t *testing.T) {
	headline, summary := extractLead("## Highlights\n- a highlight", "v0.40.0")
	if headline != "" || summary != "" {
		t.Fatalf("got %q / %q, want nothing", headline, summary)
	}
}

func TestExtractLeadBoundsTheSummary(t *testing.T) {
	body := "## v0.40.0\n" + strings.Repeat("word ", 200)
	_, summary := extractLead(body, "v0.40.0")
	if got := len([]rune(summary)); got > maxSummaryLength || !strings.HasSuffix(summary, "…") {
		t.Fatalf("summary is %d characters, want at most %d ending in an ellipsis", got, maxSummaryLength)
	}
}

func TestHighlightsKeepBalancedAccentMarks(t *testing.T) {
	body := "## Highlights\n- press `ctrl+x` to step effort\n- a stray ` mark is dropped\n"
	want := []string{"Press `ctrl+x` to step effort", "A stray  mark is dropped"}
	if got := extractHighlights(body); !slices.Equal(got, want) {
		t.Fatalf("extractHighlights() = %q, want %q", got, want)
	}
}

func TestThanksStayPlain(t *testing.T) {
	body := "## Thank you\n- @someone fixed `the thing` (#12)\n"
	want := []string{"@someone fixed the thing (#12)"}
	if got := extractThanks(body); !slices.Equal(got, want) {
		t.Fatalf("extractThanks() = %q, want %q", got, want)
	}
}

func TestTruncateCountsVisibleCharacters(t *testing.T) {
	marked := "`" + strings.Repeat("a", maxLineLength) + "`"
	if got := truncate(marked, maxLineLength); got != marked {
		t.Fatalf("marks take no cell, so %d visible characters must fit", maxLineLength)
	}
	cut := truncate("`"+strings.Repeat("a", maxLineLength+1)+"`", maxLineLength)
	if want := "`" + strings.Repeat("a", maxLineLength-1) + "`…"; cut != want {
		t.Fatalf("truncate() = %q, want the span closed before the ellipsis", cut)
	}
}

func TestCatalogCarriesTheLeadThroughTheCache(t *testing.T) {
	body := `## v0.40.0\n**Pickers are here!**\n\nA summary with ` + "`ctrl+x`" + `.\n\n## Highlights\n- a highlight\n\n## What's Changed\n* feat(ui): a feature`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `[{"tag_name":"v0.40.0","html_url":"https://github.com/YoanWai/agent-manager/releases/tag/v0.40.0","body":"%s","draft":false,"prerelease":false}]`, body)
	}))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	dir := t.TempDir()
	if _, err := Check(context.Background(), dir, "v0.39.0"); err != nil {
		t.Fatal(err)
	}
	release := Cached(dir, "v0.39.0").Releases[0]
	if release.Headline != "Pickers are here!" || release.Summary != "A summary with `ctrl+x`." {
		t.Fatalf("lead lost in the cache: %q / %q", release.Headline, release.Summary)
	}
}

func TestCatalogFileWinsOverTheLegacyFile(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{CheckedAt: time.Now(), Releases: []Release{testRelease("v0.41.0", "from the catalog")}})
	seedFile(t, dir, legacyCacheFile, legacyCatalogJSON)
	catalogBytes, err := os.ReadFile(filepath.Join(dir, cacheFile))
	if err != nil {
		t.Fatal(err)
	}

	if got := Cached(dir, "v0.39.0"); got.Latest != "v0.41.0" || len(got.Releases) != 1 {
		t.Fatalf("Cached read the legacy file: %+v", got)
	}

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.39.0")
	if err != nil || calls.Load() != 0 || result.Latest != "v0.41.0" || len(result.Releases) != 1 {
		t.Fatalf("result=%+v err=%v calls=%d, want the fresh catalog with no fetch", result, err, calls.Load())
	}
	if kept, err := os.ReadFile(filepath.Join(dir, cacheFile)); err != nil || string(kept) != string(catalogBytes) {
		t.Fatalf("the catalog file changed: %v", err)
	}
	if kept, err := os.ReadFile(filepath.Join(dir, legacyCacheFile)); err != nil || string(kept) != legacyCatalogJSON {
		t.Fatalf("the legacy file must stay byte for byte: %v", err)
	}
}

func TestParserNumberPinsTheReleaseShape(t *testing.T) {
	shapes := map[int][]string{
		1: {"version", "url", "headline", "summary", "highlights", "thanks", "changes.kind", "changes.text", "changes.author", "total_changes"},
	}
	want, ok := shapes[catalogParser]
	if !ok {
		t.Fatalf("catalogParser %d has no pinned shape: list Release's fields under it here", catalogParser)
	}
	if got := jsonKeys(reflect.TypeOf(Release{}), ""); !slices.Equal(got, want) {
		t.Fatalf("Release has fields %q, parser %d names %q: a changed field set needs a higher parser number", got, catalogParser, want)
	}
}

func jsonKeys(structType reflect.Type, prefix string) []string {
	var keys []string
	for index := range structType.NumField() {
		field := structType.Field(index)
		name := prefix + strings.Split(field.Tag.Get("json"), ",")[0]
		elem := field.Type
		if elem.Kind() == reflect.Slice {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct {
			keys = append(keys, jsonKeys(elem, name+".")...)
			continue
		}
		keys = append(keys, name)
	}
	return keys
}
