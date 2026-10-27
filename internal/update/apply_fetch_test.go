package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// tarRelease is a linux or darwin release archive, whatever this test
// runs on.
func tarRelease(t *testing.T, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipper)
	if err := archive.WriteHeader(&tar.Header{Name: "agent-manager", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveHostRelease publishes one asset and its checksum line, and counts
// the requests that reach it.
func serveHostRelease(t *testing.T, tag, asset string, archive []byte, sum string) *atomic.Int32 {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/" + tag + "/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", sum, asset)
		case "/" + tag + "/" + asset:
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	orig := downloadBase
	downloadBase = server.URL
	t.Cleanup(func() { downloadBase = orig })
	return &requests
}

func TestFetchReleaseExtractsTheHostsBuild(t *testing.T) {
	archive := tarRelease(t, []byte("linux arm64 build"))
	digest := sha256.Sum256(archive)
	serveHostRelease(t, "v9.9.9", "agent-manager_9.9.9_linux_arm64.tar.gz", archive, hex.EncodeToString(digest[:]))

	dir := t.TempDir()
	binary, err := FetchRelease(context.Background(), "v9.9.9", "linux", "arm64", dir)
	if err != nil {
		t.Fatal(err)
	}
	if binary != filepath.Join(dir, "agent-manager") {
		t.Fatalf("binary at %s, want it in %s", binary, dir)
	}
	got, err := os.ReadFile(binary)
	if err != nil || string(got) != "linux arm64 build" {
		t.Fatalf("extracted %q err %v", got, err)
	}
}

func TestFetchReleaseRejectsChecksumMismatch(t *testing.T) {
	archive := tarRelease(t, []byte("tampered build"))
	serveHostRelease(t, "v9.9.9", "agent-manager_9.9.9_darwin_amd64.tar.gz", archive, strings.Repeat("0", 64))

	dir := t.TempDir()
	_, err := FetchRelease(context.Background(), "v9.9.9", "darwin", "amd64", dir)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a mismatched archive left %v in the staging directory", entries)
	}
}

func TestFetchReleaseRefusesWhatNoReleaseHolds(t *testing.T) {
	requests := serveHostRelease(t, "v9.9.9", "unused", nil, "")
	for _, tc := range []struct{ tag, goos string }{
		{"dev", "linux"},
		{"9.9.9", "linux"},
		{"v9.9.9", "windows"},
	} {
		if _, err := FetchRelease(context.Background(), tc.tag, tc.goos, "amd64", t.TempDir()); err == nil {
			t.Errorf("FetchRelease(%q, %q) succeeded", tc.tag, tc.goos)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Fatalf("a refusal still made %d requests", n)
	}
}
