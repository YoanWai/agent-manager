package feed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/atomicfile"
	"github.com/YoanWai/agent-manager/internal/update"
)

func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })
	return server
}

func fetch(t *testing.T, dir, version string) []Message {
	t.Helper()
	messages, err := Fetch(t.Context(), dir, version)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	return messages
}

func TestFetchParsesAndPrefixesIDs(t *testing.T) {
	serve(t, `[{"id":"holdoff-0142","banner":"known issue in v0.14.2","title":"Hold off on v0.14.2","body":["Sessions may drop.","Fixed in v0.14.3."],"url":"https://github.com/YoanWai/agent-manager/issues/200"}]`)
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.ID != "feed-holdoff-0142" {
		t.Fatalf("ids must be namespaced, got %q", msg.ID)
	}
	if msg.Banner != "known issue in v0.14.2" || msg.Title != "Hold off on v0.14.2" {
		t.Fatalf("round trip mismatch: %+v", msg)
	}
	if len(msg.Body) != 2 {
		t.Fatalf("want 2 body lines, got %v", msg.Body)
	}
}

func TestFetchDropsInvalidEntries(t *testing.T) {
	serve(t, `[
		{"id":"ok","banner":"fine","title":"Fine","url":"https://example.com"},
		{"id":"","banner":"no id","title":"x"},
		{"id":"BAD ID!","banner":"bad id","title":"x"},
		{"id":"bad-url","banner":"x","title":"x","url":"file:///etc/passwd"},
		{"id":"sneaky-url","banner":"x","title":"x","url":"https://evil.com/\u001b[2Jx"},
		{"id":"hostless-url","banner":"x","title":"x","url":"https://"},
		{"id":"no-banner","title":"x"}
	]`)
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 1 || messages[0].ID != "feed-ok" {
		t.Fatalf("only the valid entry should survive, got %+v", messages)
	}
}

func TestFetchStripsControlSequences(t *testing.T) {
	serve(t, `[{"id":"tricky","banner":"evil \u001b[31mred\u001b[0m\ttext","title":"a\u001b]0;owned\u0007b"}]`)
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(messages))
	}
	if strings.ContainsAny(messages[0].Banner+messages[0].Title, "\x1b\x07\t") {
		t.Fatalf("control sequences must not survive: %+v", messages[0])
	}
	if !strings.Contains(messages[0].Banner, "red") {
		t.Fatalf("text content should survive stripping, got %q", messages[0].Banner)
	}
}

func TestFetchHonorsVersionBounds(t *testing.T) {
	serve(t, `[
		{"id":"old-only","banner":"x","title":"x","max_version":"v0.14.1"},
		{"id":"new-only","banner":"x","title":"x","min_version":"v0.14.2"},
		{"id":"wildcard-bound","banner":"x","title":"x","max_version":"0.14.x"}
	]`)
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 1 || messages[0].ID != "feed-new-only" {
		t.Fatalf("bounds should filter, got %+v", messages)
	}
}

func TestFetchHonorsExpiry(t *testing.T) {
	serve(t, `[
		{"id":"expired","banner":"x","title":"x","expires_at":"2020-01-01T00:00:00Z"},
		{"id":"future","banner":"x","title":"x","expires_at":"2999-01-01T00:00:00Z"},
		{"id":"invalid","banner":"x","title":"x","expires_at":"someday"},
		{"id":"open","banner":"x","title":"x"}
	]`)
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 2 || messages[0].ID != "feed-future" || messages[1].ID != "feed-open" {
		t.Fatalf("expiry filtering = %+v", messages)
	}
}

func TestFetchCachesBetweenCalls(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Write([]byte(`[{"id":"one","banner":"x","title":"x"}]`))
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	dir := t.TempDir()
	fetch(t, dir, "v0.14.2")
	fetch(t, dir, "v0.14.2")
	if got := hits.Load(); got != 1 {
		t.Fatalf("second fetch should come from the cache, got %d hits", got)
	}
}

func TestRefreshBypassesCache(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Write([]byte(`[{"id":"one","banner":"x","title":"x"}]`))
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	dir := t.TempDir()
	fetch(t, dir, "v0.14.2")
	if _, err := Refresh(t.Context(), dir, "v0.14.2"); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("manual refresh should bypass the cache, got %d hits", got)
	}
}

func TestFetchRefetchesFutureDatedCache(t *testing.T) {
	dir := t.TempDir()
	_ = atomicfile.WriteJSON(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now().Add(time.Hour),
		Parser:    feedParser,
		Messages:  []rawMessage{{ID: "stale", Banner: "x", Title: "x"}},
	}, 0o644)
	serve(t, `[{"id":"fresh","banner":"x","title":"x"}]`)

	messages := fetch(t, dir, "v0.14.2")
	if len(messages) != 1 || messages[0].ID != "feed-fresh" {
		t.Fatalf("future-dated cache was trusted: %+v", messages)
	}
}

func TestRefreshUsesConditionalRequest(t *testing.T) {
	dir := t.TempDir()
	_ = atomicfile.WriteJSON(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now().Add(-checkInterval),
		Parser:    feedParser,
		ETag:      `"feed-1"`,
		Messages:  []rawMessage{{ID: "one", Banner: "x", Title: "x"}},
	}, 0o644)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != `"feed-1"` {
			t.Errorf("If-None-Match = %q", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	messages, err := Refresh(t.Context(), dir, "v0.14.2")
	if err != nil || len(messages) != 1 || messages[0].ID != "feed-one" {
		t.Fatalf("messages=%+v err=%v", messages, err)
	}
	if written, ok := atomicfile.ReadJSON[cache](filepath.Join(dir, cacheFile)); !ok || written.Parser != feedParser || written.ETag != `"feed-1"` {
		t.Fatalf("a not-modified answer must keep the parser stamp: %+v", written)
	}
}

func TestFetchServesStaleCacheOnNetworkFailure(t *testing.T) {
	server := serve(t, `[{"id":"one","banner":"x","title":"x"}]`)
	dir := t.TempDir()
	fetch(t, dir, "v0.14.2")
	server.Close()

	cachePath := filepath.Join(dir, cacheFile)
	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	stale := regexp.MustCompile(`"checked_at":"\d{4}`).ReplaceAllString(string(raw), `"checked_at":"1999`)
	if stale == string(raw) {
		t.Fatal("failed to age the cache timestamp")
	}
	if err := os.WriteFile(cachePath, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}

	messages := fetch(t, dir, "v0.14.2")
	if len(messages) != 1 {
		t.Fatalf("a dead feed should fall back to the stale cache, got %+v", messages)
	}
}

func TestFetchCapsCountAndSize(t *testing.T) {
	var entries []string
	for i := 0; i < 40; i++ {
		entries = append(entries, `{"id":"n`+string(rune('a'+i%26))+string(rune('a'+i/26))+`","banner":"x","title":"x"}`)
	}
	serve(t, "["+strings.Join(entries, ",")+"]")
	messages := fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) > maxMessages {
		t.Fatalf("count must be capped at %d, got %d", maxMessages, len(messages))
	}

	long := strings.Repeat("y", 500)
	serve(t, `[{"id":"long","banner":"`+long+`","title":"`+long+`"}]`)
	messages = fetch(t, t.TempDir(), "v0.14.2")
	if len(messages) != 1 {
		t.Fatalf("want 1 message, got %d", len(messages))
	}
	if len(messages[0].Banner) > maxBannerLen || len(messages[0].Title) > maxTitleLen {
		t.Fatalf("field lengths must be capped, got banner=%d title=%d", len(messages[0].Banner), len(messages[0].Title))
	}
}

func TestFetchCarriesHeadlineAndAccent(t *testing.T) {
	serve(t, `[{"id":"pickers","banner":"Pickers are here","title":"v0.40.0: pickers are here","headline":"Model + reasoning pickers are here!","accent":["ctrl+l","ctrl+x"],"body":["ctrl+l opens the model list and ctrl+x steps the effort."]}]`)
	msg := fetch(t, t.TempDir(), "v0.40.0")[0]
	if msg.Headline != "Model + reasoning pickers are here!" {
		t.Fatalf("headline = %q", msg.Headline)
	}
	if !slices.Equal(msg.Accent, []string{"ctrl+l", "ctrl+x"}) {
		t.Fatalf("accent = %q", msg.Accent)
	}
}

func TestFetchBoundsHeadlineAndAccent(t *testing.T) {
	phrases := []string{`""`, `"` + strings.Repeat("y", maxAccentLen+20) + `"`}
	for index := 0; index < maxAccents+2; index++ {
		phrases = append(phrases, `"phrase `+strconv.Itoa(index)+`"`)
	}
	serve(t, `[{"id":"bounded","banner":"x","title":"x","headline":"`+strings.Repeat("h", maxHeadlineLen+20)+`","accent":[`+strings.Join(phrases, ",")+`]}]`)
	msg := fetch(t, t.TempDir(), "v0.40.0")[0]
	if len([]rune(msg.Headline)) != maxHeadlineLen {
		t.Fatalf("headline is %d characters, want %d", len([]rune(msg.Headline)), maxHeadlineLen)
	}
	if len(msg.Accent) != maxAccents {
		t.Fatalf("kept %d accent phrases, want %d", len(msg.Accent), maxAccents)
	}
	if slices.Contains(msg.Accent, "") {
		t.Fatalf("an empty phrase would match everywhere: %q", msg.Accent)
	}
	if got := len([]rune(msg.Accent[0])); got != maxAccentLen {
		t.Fatalf("a long phrase is %d characters, want it cut to %d", got, maxAccentLen)
	}
}

func TestFetchKeepsSixteenBodyLines(t *testing.T) {
	lines := make([]string, 0, maxBodyLines+4)
	for index := 0; index < maxBodyLines+4; index++ {
		lines = append(lines, `"line `+strconv.Itoa(index)+`"`)
	}
	serve(t, `[{"id":"long","banner":"x","title":"x","body":[`+strings.Join(lines, ",")+`]}]`)
	if got := len(fetch(t, t.TempDir(), "v0.40.0")[0].Body); got != 16 {
		t.Fatalf("kept %d body lines, want 16", got)
	}
}

// The panel releases before v0.40.0 draw shows this much of an entry.
const (
	legacyBodyLines = 8
	legacyBodyLine  = 120
	firstWidePanel  = "0.40.0"
)

func TestShippedFeedFileIsRenderableAndRetires(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "messages.json"))
	if err != nil {
		t.Fatalf("read shipped feed: %v", err)
	}
	var entries []rawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("shipped feed must parse or every install falls back to a stale cache: %v", err)
	}
	now := time.Now()
	for _, entry := range entries {
		if !validFeedID.MatchString(entry.ID) {
			t.Errorf("%q: invalid id", entry.ID)
		}
		if entry.URL != "" && !safeURL(entry.URL) {
			t.Errorf("%s: unsafe url %q", entry.ID, entry.URL)
		}
		if got := cleanText(entry.Banner, maxBannerLen); got != entry.Banner {
			t.Errorf("%s: banner is cut to %q", entry.ID, got)
		}
		if got := cleanText(entry.Title, maxTitleLen); got != entry.Title {
			t.Errorf("%s: title is cut to %q", entry.ID, got)
		}
		bodyLines, lineLength := maxBodyLines, maxBodyLine
		if entry.MinVersion == "" || update.Newer(firstWidePanel, entry.MinVersion) {
			bodyLines, lineLength = legacyBodyLines, legacyBodyLine
		}
		if len(entry.Body) > bodyLines {
			t.Errorf("%s: %d body lines, only the first %d render for the versions it reaches", entry.ID, len(entry.Body), bodyLines)
		}
		for i, line := range entry.Body {
			if got := cleanText(line, lineLength); got != line {
				t.Errorf("%s: body line %d is cut to %q for the versions it reaches", entry.ID, i+1, got)
			}
		}
		if got := cleanText(entry.Headline, maxHeadlineLen); got != entry.Headline {
			t.Errorf("%s: headline is cut to %q", entry.ID, got)
		}
		if len(entry.Accent) > maxAccents {
			t.Errorf("%s: %d accent phrases, only the first %d apply", entry.ID, len(entry.Accent), maxAccents)
		}
		for _, phrase := range entry.Accent {
			if got := cleanText(phrase, maxAccentLen); got != phrase || phrase == "" {
				t.Errorf("%s: accent phrase %q is cut to %q", entry.ID, phrase, got)
			}
			if !slices.ContainsFunc(entry.Body, func(line string) bool { return strings.Contains(line, phrase) }) {
				t.Errorf("%s: accent phrase %q is in no body line", entry.ID, phrase)
			}
		}
		if entry.MaxVersion == "" && entry.ExpiresAt == "" {
			t.Errorf("%s: needs max_version or expires_at, or it keeps showing after it stops being true", entry.ID)
		}
		if entry.MaxVersion != "" {
			if !serves(sanitize(entries, entry.MaxVersion, now), entry.ID) {
				t.Errorf("%s: max_version %q hides it from its own release; a bound that fails to parse hides it from everyone", entry.ID, entry.MaxVersion)
			}
			if above := majorAbove(t, entry.MaxVersion); serves(sanitize(entries, above, now), entry.ID) {
				t.Errorf("%s: still served on %s", entry.ID, above)
			}
		}
		if entry.ExpiresAt != "" {
			if _, err := time.Parse(time.RFC3339, entry.ExpiresAt); err != nil {
				t.Errorf("%s: expires_at %q does not parse, which drops the entry: %v", entry.ID, entry.ExpiresAt, err)
			}
		}
	}
}

func serves(messages []Message, id string) bool {
	for _, msg := range messages {
		if msg.ID == "feed-"+id {
			return true
		}
	}
	return false
}

func majorAbove(t *testing.T, version string) string {
	t.Helper()
	fields := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(fields) != 3 {
		t.Fatalf("version bound %q is not three fields", version)
	}
	major, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("version bound %q has a non-numeric major field", version)
	}
	return strconv.Itoa(major+1) + "." + fields[1] + "." + fields[2]
}

const legacyFeedJSON = `{"checked_at":"2099-01-01T00:00:00Z","etag":"\"legacy-feed\"","messages":[{"id":"one","banner":"x","title":"Seeded","body":["from the older build"]}]}`

func TestFeedIsCachedInItsOwnFile(t *testing.T) {
	serve(t, `[{"id":"one","banner":"x","title":"x"}]`)
	dir := t.TempDir()
	fetch(t, dir, "v0.40.0")
	written, ok := atomicfile.ReadJSON[cache](filepath.Join(dir, "feed-messages.json"))
	if !ok || written.Parser != feedParser || len(written.Messages) != 1 {
		t.Fatalf("feed not cached with its parser: %+v", written)
	}
	if _, err := os.Stat(filepath.Join(dir, "message-feed.json")); !os.IsNotExist(err) {
		t.Fatalf("the file older builds own must be left alone: %v", err)
	}
}

func TestFeedFromAnotherParserIsRefetchedWithoutItsETag(t *testing.T) {
	dir := t.TempDir()
	_ = atomicfile.WriteJSON(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now(),
		Parser:    feedParser + 1,
		ETag:      `"other-parser"`,
		Messages:  []rawMessage{{ID: "one", Banner: "x", Title: "stale"}},
	}, 0o644)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("If-None-Match = %q, want none", got)
		}
		w.Write([]byte(`[{"id":"one","banner":"x","title":"fresh","accent":["fresh"],"body":["fresh text"]}]`))
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	messages := fetch(t, dir, "v0.40.0")
	if hits.Load() != 1 || messages[0].Title != "fresh" || len(messages[0].Accent) != 1 {
		t.Fatalf("hits=%d messages=%+v, want one full fetch despite the fresh timestamp", hits.Load(), messages)
	}
}

func TestLegacyFeedSeedsUntilTheFirstFetch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "message-feed.json"), []byte(legacyFeedJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	messages := fetch(t, dir, "v0.40.0")
	if len(messages) != 1 || messages[0].Title != "Seeded" {
		t.Fatalf("the older build's feed should show while the fetch fails: %+v", messages)
	}
	kept, err := os.ReadFile(filepath.Join(dir, "message-feed.json"))
	if err != nil || string(kept) != legacyFeedJSON {
		t.Fatalf("the legacy file must stay byte for byte: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, cacheFile)); !os.IsNotExist(err) {
		t.Fatalf("a failed fetch must not write a cache: %v", err)
	}
}

func TestFeedFileWinsOverTheLegacyFile(t *testing.T) {
	dir := t.TempDir()
	_ = atomicfile.WriteJSON(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now(),
		Parser:    feedParser,
		Messages:  []rawMessage{{ID: "one", Banner: "x", Title: "From the new file"}},
	}, 0o644)
	if err := os.WriteFile(filepath.Join(dir, legacyCacheFile), []byte(legacyFeedJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	messages := fetch(t, dir, "v0.40.0")
	if hits.Load() != 0 || len(messages) != 1 || messages[0].Title != "From the new file" {
		t.Fatalf("hits=%d messages=%+v, want the fresh new file with no fetch", hits.Load(), messages)
	}
	if kept, err := os.ReadFile(filepath.Join(dir, legacyCacheFile)); err != nil || string(kept) != legacyFeedJSON {
		t.Fatalf("the legacy file must stay byte for byte: %v", err)
	}
}

func TestLegacyFeedSeedIsFetchedAtOnceWithoutItsETag(t *testing.T) {
	dir := t.TempDir()
	seed := `{"checked_at":"` + time.Now().Format(time.RFC3339) + `","etag":"\"legacy-feed\"","messages":[{"id":"one","banner":"x","title":"Seeded"}]}`
	if err := os.WriteFile(filepath.Join(dir, legacyCacheFile), []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("If-None-Match"); got != "" {
			t.Errorf("If-None-Match = %q, want none: a not-modified answer would keep the older build's parse", got)
		}
		w.Write([]byte(`[{"id":"one","banner":"x","title":"Fetched"}]`))
	}))
	t.Cleanup(server.Close)
	old := feedURL
	feedURL = server.URL
	t.Cleanup(func() { feedURL = old })

	messages := fetch(t, dir, "v0.40.0")
	if hits.Load() != 1 || len(messages) != 1 || messages[0].Title != "Fetched" {
		t.Fatalf("hits=%d messages=%+v, want one fetch at once: a seed is never fresh", hits.Load(), messages)
	}
	written, ok := atomicfile.ReadJSON[cache](filepath.Join(dir, cacheFile))
	if !ok || written.Parser != feedParser || len(written.Messages) != 1 {
		t.Fatalf("feed not cached with its parser: %+v", written)
	}
}

func TestParserNumberPinsTheEntryShape(t *testing.T) {
	shapes := map[int][]string{
		1: {"id", "banner", "title", "headline", "accent", "body", "url", "min_version", "max_version", "expires_at"},
	}
	want, ok := shapes[feedParser]
	if !ok {
		t.Fatalf("feedParser %d has no pinned shape: list rawMessage's fields under it here", feedParser)
	}
	var got []string
	entryType := reflect.TypeOf(rawMessage{})
	for index := range entryType.NumField() {
		got = append(got, strings.Split(entryType.Field(index).Tag.Get("json"), ",")[0])
	}
	if !slices.Equal(got, want) {
		t.Fatalf("rawMessage has fields %q, parser %d names %q: a changed field set needs a higher parser number", got, feedParser, want)
	}
}
