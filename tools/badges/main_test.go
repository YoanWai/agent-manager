package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompact(t *testing.T) {
	for _, tc := range []struct {
		in   int
		want string
	}{
		{0, "0"},
		{176, "176"},
		{999, "999"},
		{1000, "1k"},
		{1918, "1.9k"},
		{12500, "12.5k"},
		{100000, "100k"},
	} {
		if got := compact(tc.in); got != tc.want {
			t.Errorf("compact(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// serveTraffic points the API at a local server that answers the clone
// traffic call with status and every other call with 404, so run() leaves
// the contributor image alone.
func serveTraffic(t *testing.T, status int) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+repo+"/traffic/clones" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte(`{"count":1918}`))
		}
	}))
	t.Cleanup(server.Close)
	previous := apiBase
	apiBase = server.URL
	t.Cleanup(func() { apiBase = previous })
	t.Chdir(t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
}

func TestRunFailsWhenBadgeTokenIsRejected(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			serveTraffic(t, status)
			t.Setenv("BADGE_TOKEN", "rejected")
			err := run()
			if err == nil {
				t.Fatal("run() = nil, want an error for a rejected BADGE_TOKEN")
			}
			if !strings.Contains(err.Error(), "BADGE_TOKEN") || !strings.Contains(err.Error(), http.StatusText(status)) {
				t.Fatalf("run() error = %q, want it to name BADGE_TOKEN and the status", err)
			}
			assertNoCloneEndpoint(t)
		})
	}
}

func TestRunKeepsPublishedCountWithoutBadgeToken(t *testing.T) {
	serveTraffic(t, http.StatusForbidden)
	t.Setenv("BADGE_TOKEN", "")
	if err := run(); err != nil {
		t.Fatalf("run() = %v, want nil without BADGE_TOKEN", err)
	}
	assertNoCloneEndpoint(t)
}

func TestRunWritesCloneEndpoint(t *testing.T) {
	serveTraffic(t, http.StatusOK)
	t.Setenv("BADGE_TOKEN", "valid")
	if err := run(); err != nil {
		t.Fatalf("run() = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(outDir, "clones.json"))
	if err != nil {
		t.Fatal(err)
	}
	var endpoint struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &endpoint); err != nil {
		t.Fatal(err)
	}
	if endpoint.Message != "1.9k" {
		t.Fatalf("clones.json message = %q, want 1.9k", endpoint.Message)
	}
}

func assertNoCloneEndpoint(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(outDir, "clones.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("clones.json stat error = %v, want it absent", err)
	}
}
