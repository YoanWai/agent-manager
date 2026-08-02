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
	"runtime"
	"strings"
	"testing"
)

func releaseArchive(t *testing.T, binary []byte) []byte {
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

func serveRelease(t *testing.T, tag string, archive []byte, sum string) {
	t.Helper()
	asset := fmt.Sprintf("agent-manager_%s_%s_%s.tar.gz", strings.TrimPrefix(tag, "v"), runtime.GOOS, runtime.GOARCH)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

func TestApplySwapsBinary(t *testing.T) {
	newBinary := []byte("new build bytes")
	archive := releaseArchive(t, newBinary)
	digest := sha256.Sum256(archive)
	serveRelease(t, "v9.9.9", archive, hex.EncodeToString(digest[:]))

	target := filepath.Join(t.TempDir(), "agent-manager")
	if err := os.WriteFile(target, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), "v9.9.9", target); err != nil {
		t.Fatalf("apply: %v", err)
	}
	swapped, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(swapped, newBinary) {
		t.Fatalf("binary not swapped, got %q", swapped)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("swapped binary mode = %v", info.Mode().Perm())
	}
}

func TestApplyFollowsSymlink(t *testing.T) {
	newBinary := []byte("new build bytes")
	archive := releaseArchive(t, newBinary)
	digest := sha256.Sum256(archive)
	serveRelease(t, "v9.9.9", archive, hex.EncodeToString(digest[:]))

	dir := t.TempDir()
	target := filepath.Join(dir, "real-binary")
	if err := os.WriteFile(target, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "agent-manager")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), "v9.9.9", link); err != nil {
		t.Fatalf("apply: %v", err)
	}
	swapped, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(swapped, newBinary) {
		t.Fatal("symlink target should hold the new build")
	}
	wantTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(link); err != nil || resolved != wantTarget {
		t.Fatalf("link should survive the swap, resolved %q err %v", resolved, err)
	}
}

func TestApplyRejectsChecksumMismatch(t *testing.T) {
	archive := releaseArchive(t, []byte("new build bytes"))
	serveRelease(t, "v9.9.9", archive, strings.Repeat("0", 64))

	target := filepath.Join(t.TempDir(), "agent-manager")
	if err := os.WriteFile(target, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), "v9.9.9", target)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch, got %v", err)
	}
	untouched, readErr := os.ReadFile(target)
	if readErr != nil || string(untouched) != "old build" {
		t.Fatalf("binary must stay untouched on mismatch, got %q err %v", untouched, readErr)
	}
}

func TestApplyRejectsArchiveWithoutBinary(t *testing.T) {
	var buf bytes.Buffer
	zipper := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipper)
	if err := archive.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	archive.Close()
	zipper.Close()
	digest := sha256.Sum256(buf.Bytes())
	serveRelease(t, "v9.9.9", buf.Bytes(), hex.EncodeToString(digest[:]))

	target := filepath.Join(t.TempDir(), "agent-manager")
	if err := os.WriteFile(target, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := Apply(context.Background(), "v9.9.9", target)
	if err == nil || !strings.Contains(err.Error(), "no agent-manager binary") {
		t.Fatalf("want missing-binary error, got %v", err)
	}
}
