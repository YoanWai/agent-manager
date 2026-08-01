// Package feed pulls the project's remote message feed: a small JSON
// file the maintainer edits to talk to running installs between
// releases (known issues, tips, announcements). Fetches are cached on
// disk and throttled like the release check, and every field of the
// untrusted payload is validated and stripped before the TUI renders it.
package feed

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
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/update"
	"github.com/charmbracelet/x/ansi"
)

const (
	cacheFile     = "message-feed.json"
	checkInterval = 6 * time.Hour
	requestBudget = 4 * time.Second

	maxPayload    = 64 << 10
	maxMessages   = 16
	maxBannerLen  = 60
	maxTitleLen   = 80
	maxBodyLines  = 8
	maxBodyLine   = 120
	maxURLLen     = 200
	feedIDPattern = `^[a-z0-9][a-z0-9-]{0,63}$`
)

// feedURL is a var so tests can point the fetch at a local server.
var feedURL = "https://raw.githubusercontent.com/YoanWai/agent-manager/main/docs/messages.json"

var validFeedID = regexp.MustCompile(feedIDPattern)

// Message is one entry of the remote feed, already validated and safe to
// render. ID carries a "feed-" prefix so remote entries can never collide
// with the notices built into the binary.
type Message struct {
	ID     string
	Banner string
	Title  string
	Body   []string
	URL    string
}

type rawMessage struct {
	ID         string   `json:"id"`
	Banner     string   `json:"banner"`
	Title      string   `json:"title"`
	Body       []string `json:"body"`
	URL        string   `json:"url"`
	MinVersion string   `json:"min_version"`
	MaxVersion string   `json:"max_version"`
}

type cache struct {
	CheckedAt time.Time    `json:"checked_at"`
	Messages  []rawMessage `json:"messages"`
}

// Fetch returns the feed messages that apply to this build. It serves
// from the on-disk cache within checkInterval, and falls back to the
// stale cache when the network fails, so the TUI never blocks or loses
// messages to a flaky connection.
func Fetch(ctx context.Context, configDir, version string) ([]Message, error) {
	cachePath := filepath.Join(configDir, cacheFile)
	cached, haveCache := readCache(cachePath)
	if haveCache && time.Since(cached.CheckedAt) < checkInterval {
		return sanitize(cached.Messages, version), nil
	}

	raw, err := download(ctx)
	if err != nil {
		if haveCache {
			return sanitize(cached.Messages, version), nil
		}
		return nil, err
	}
	writeCache(cachePath, cache{CheckedAt: time.Now(), Messages: raw})
	return sanitize(raw, version), nil
}

func download(ctx context.Context) ([]rawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, requestBudget)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("feed: server returned %s", resp.Status)
	}

	var raw []rawMessage
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPayload)).Decode(&raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// sanitize turns the untrusted payload into renderable messages: entries
// with a bad id, a non-https url, or outside their version bounds are
// dropped whole; surviving text is stripped of terminal control
// sequences and cut to bounded lengths.
func sanitize(raw []rawMessage, version string) []Message {
	var messages []Message
	for _, entry := range raw {
		if len(messages) == maxMessages {
			break
		}
		if !validFeedID.MatchString(entry.ID) {
			continue
		}
		if entry.URL != "" && !safeURL(entry.URL) {
			continue
		}
		if !update.VersionWithin(version, entry.MinVersion, entry.MaxVersion) {
			continue
		}
		msg := Message{
			ID:     "feed-" + entry.ID,
			Banner: cleanText(entry.Banner, maxBannerLen),
			Title:  cleanText(entry.Title, maxTitleLen),
			URL:    entry.URL,
		}
		if msg.Banner == "" || msg.Title == "" {
			continue
		}
		for _, line := range entry.Body {
			if len(msg.Body) == maxBodyLines {
				break
			}
			if line = cleanText(line, maxBodyLine); line != "" {
				msg.Body = append(msg.Body, line)
			}
		}
		messages = append(messages, msg)
	}
	return messages
}

// safeURL admits only an https URL with a real host that is already
// clean: one that stripping control sequences would alter is rejected
// whole rather than repaired, because a repaired URL is a different URL.
// The check guards both the modal's link line and the argument later
// handed to the OS opener.
func safeURL(raw string) bool {
	if raw != cleanText(raw, maxURLLen) {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

// cleanText makes one remote string safe for a terminal: ANSI sequences
// stripped, remaining control characters dropped, length bounded.
func cleanText(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range ansi.Strip(s) {
		if r < ' ' || r == 0x7f {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if runes := []rune(out); len(runes) > maxLen {
		out = string(runes[:maxLen])
	}
	return out
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
