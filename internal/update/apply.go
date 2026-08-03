package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// downloadBase is a var so tests can point the release download at a
// local server.
var downloadBase = "https://github.com/YoanWai/agent-manager/releases/download"

var buildSource string

func SetBuildSource(source string) {
	buildSource = source
}

const applyBudget = 2 * time.Minute

// binaryLimit caps how much of the archive is read into the new binary,
// so a corrupt or hostile archive cannot fill the disk. Real builds are
// under 20MB.
const binaryLimit = 200 << 20

var errHomebrewManaged = errors.New("installed via Homebrew; upgrade with: brew upgrade yoanwai/tap/agent-manager")

func homebrewManaged(source, execPath string) bool {
	if source == "Homebrew" {
		return true
	}
	path := execPath
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		path = resolved
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i+1] != "agent-manager" {
			continue
		}
		switch parts[i] {
		case "Cellar", "Caskroom", "opt":
			return true
		}
	}
	return false
}

// Apply downloads the release named by tag, verifies the archive for this
// OS and architecture against the release's checksums.txt, and atomically
// replaces the binary at execPath. The swap uses rename, so the running
// process keeps its (now unlinked) image and the next start runs the new
// build under a fresh inode, which also sidesteps macOS's per-inode
// signature cache.
func Apply(ctx context.Context, tag, execPath string) error {
	if homebrewManaged(buildSource, execPath) {
		return errHomebrewManaged
	}

	ctx, cancel := context.WithTimeout(ctx, applyBudget)
	defer cancel()

	version := strings.TrimPrefix(tag, "v")
	asset := fmt.Sprintf("agent-manager_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)

	wantSum, err := fetchChecksum(ctx, tag, asset)
	if err != nil {
		return err
	}

	archive, err := fetchAsset(ctx, tag, asset)
	if err != nil {
		return err
	}
	defer os.Remove(archive)

	if err := verifyChecksum(archive, wantSum); err != nil {
		return err
	}

	target, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}
	staged := target + ".update"
	if err := extractBinary(archive, staged); err != nil {
		return err
	}
	if err := os.Rename(staged, target); err != nil {
		os.Remove(staged)
		return err
	}
	return nil
}

func fetchChecksum(ctx context.Context, tag, asset string) (string, error) {
	body, err := fetch(ctx, downloadBase+"/"+tag+"/checksums.txt")
	if err != nil {
		return "", err
	}
	defer body.Close()
	raw, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("update: checksums.txt has no entry for %s", asset)
}

// fetchAsset streams the release archive to a temp file and returns its path.
func fetchAsset(ctx context.Context, tag, asset string) (string, error) {
	body, err := fetch(ctx, downloadBase+"/"+tag+"/"+asset)
	if err != nil {
		return "", err
	}
	defer body.Close()
	tmp, err := os.CreateTemp("", "agent-manager-update-*.tar.gz")
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, io.LimitReader(body, binaryLimit)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

func fetch(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("update: %s returned %s", url, resp.Status)
	}
	return resp.Body, nil
}

func verifyChecksum(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if got != want {
		return fmt.Errorf("update: checksum mismatch: got %s, want %s", got, want)
	}
	return nil
}

// extractBinary writes the archive's agent-manager entry to staged,
// executable, next to the target so the final rename stays on one
// filesystem.
func extractBinary(archive, staged string) error {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	unzipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer unzipped.Close()
	entries := tar.NewReader(unzipped)
	for {
		header, err := entries.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("update: archive holds no agent-manager binary")
		}
		if err != nil {
			return err
		}
		if filepath.Base(header.Name) != "agent-manager" || header.Typeflag != tar.TypeReg {
			continue
		}
		out, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, io.LimitReader(entries, binaryLimit)); err != nil {
			out.Close()
			os.Remove(staged)
			return err
		}
		return out.Close()
	}
}
