package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		"- feat(ui): add a [message browser](https://example.com) with `scrolling`",
		"- feat(mcp-editor): expose tool capabilities",
		"",
		"**Full Changelog**: https://github.com/example",
		"* fix(hidden): do not include this",
	}, "\n")

	changes, total := extractChanges(body)
	want := []string{
		"Groups: Make group creation immediate and reliable",
		"Config: Add Pi as a built-in tool · @steveprentice",
		"Deps: Bump gopsutil",
		"UI: Add a message browser with scrolling",
		"MCP editor: Expose tool capabilities",
	}
	if total != len(want) || fmt.Sprint(changes) != fmt.Sprint(want) {
		t.Fatalf("extractChanges() = %q total %d, want %q", changes, total, want)
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
	if strings.Contains(changes[0], "\x1b") {
		t.Fatalf("terminal control sequence survived: %q", changes[0])
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
	if got := result.Releases[0].Changes; len(got) != 1 || got[0] != "UI: Actual latest" {
		t.Fatalf("release changes = %q", got)
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
	if !ok || !written.CheckedAt.After(oldCheckedAt) || written.ETag != `"catalog-1"` {
		t.Fatalf("conditional refresh did not advance cache: %+v", written)
	}
}

func TestLegacyCacheWithoutCatalogRefetches(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{CheckedAt: time.Now(), Latest: "v0.9.0", URL: "https://github.com/old"})
	var calls atomic.Int32
	server := releaseServer(t, &calls, "v0.10.0")
	defer server.Close()
	defer swapReleasesURL(server.URL)()

	result, err := Check(context.Background(), dir, "v0.8.2")
	if err != nil || calls.Load() != 1 || result.Latest != "v0.10.0" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls.Load())
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

func TestCachedNeverTouchesNetworkOrTrustsLegacyShape(t *testing.T) {
	dir := t.TempDir()
	seedCache(t, dir, cache{CheckedAt: time.Now(), Latest: "v0.9.0"})
	if result := Cached(dir, "v0.8.2"); len(result.Releases) != 0 {
		t.Fatalf("legacy cache should be ignored: %+v", result)
	}
	seedCache(t, dir, cache{CheckedAt: time.Now(), Releases: []Release{testRelease("v0.9.0", "new")}})
	if result := Cached(dir, "v0.8.2"); result.Latest != "v0.9.0" {
		t.Fatalf("cached catalog not returned: %+v", result)
	}
}

func testRelease(version string, changes ...string) Release {
	return Release{
		Version:      version,
		URL:          "https://github.com/YoanWai/agent-manager/releases/tag/" + version,
		Changes:      changes,
		TotalChanges: len(changes),
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
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, cacheFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

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
