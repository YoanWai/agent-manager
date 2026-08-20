package feed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
		{"id":"new-only","banner":"x","title":"x","min_version":"v0.14.2"}
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
	writeCache(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now().Add(time.Hour),
		Messages:  []rawMessage{{ID: "stale", Banner: "x", Title: "x"}},
	})
	serve(t, `[{"id":"fresh","banner":"x","title":"x"}]`)

	messages := fetch(t, dir, "v0.14.2")
	if len(messages) != 1 || messages[0].ID != "feed-fresh" {
		t.Fatalf("future-dated cache was trusted: %+v", messages)
	}
}

func TestRefreshUsesConditionalRequest(t *testing.T) {
	dir := t.TempDir()
	writeCache(filepath.Join(dir, cacheFile), cache{
		CheckedAt: time.Now().Add(-checkInterval),
		ETag:      `"feed-1"`,
		Messages:  []rawMessage{{ID: "one", Banner: "x", Title: "x"}},
	})
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

func TestShippedFeedFileIsRenderableAndRetires(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "messages.json"))
	if err != nil {
		t.Fatalf("read shipped feed: %v", err)
	}
	var entries []rawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("shipped feed must parse or every install falls back to a stale cache: %v", err)
	}
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
		if len(entry.Body) > maxBodyLines {
			t.Errorf("%s: %d body lines, only the first %d render", entry.ID, len(entry.Body), maxBodyLines)
		}
		for i, line := range entry.Body {
			if got := cleanText(line, maxBodyLine); got != line {
				t.Errorf("%s: body line %d is cut to %q", entry.ID, i+1, got)
			}
		}
		if entry.MaxVersion == "" && entry.ExpiresAt == "" {
			t.Errorf("%s: needs max_version or expires_at, or it keeps showing after it stops being true", entry.ID)
		}
	}
}
