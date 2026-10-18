package remote

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// Fetcher downloads the release named by tag for goos and goarch into dir,
// verified against the release's published checksums, and returns the
// binary's path. update.FetchRelease is the real one.
type Fetcher func(ctx context.Context, tag, goos, goarch, dir string) (string, error)

const (
	// installTimeout covers streaming a release build over a slow link.
	installTimeout = 5 * time.Minute
	// installDir is the install script's default for a user without root,
	// under $HOME on the host.
	installDir = ".local/bin"
)

var releaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)

// InstallTag is the release a host is given: the one this build was cut
// from. A dev build has none, and the release of some other version
// could not be trusted to speak this build's protocol.
func InstallTag(version string) (string, error) {
	if !releaseVersion.MatchString(version) {
		return "", fmt.Errorf("this agent-manager (%s) is not a release, so there is no published build to install; install agent-manager on that host by hand", cleanText(version))
	}
	return "v" + strings.TrimPrefix(version, "v"), nil
}

// Platform is a host's OS and architecture, in Go's names.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

var errWindowsHost = errors.New("Windows hosts are not supported: SSH connections run agent-manager through a POSIX login shell there")

// Platform asks the host's login shell what it runs on.
func (c *Client) Platform(ctx context.Context, host string) (Platform, error) {
	out, err := c.exec(ctx, host, callTimeout, nil, []string{"uname", "-sm"})
	if err != nil {
		if Unreachable(err) {
			return Platform{}, err
		}
		return Platform{}, &Error{Host: host, Err: fmt.Errorf("could not run uname in its login shell (%v); it needs a POSIX one, and a Windows host is not supported", errors.Unwrap(err))}
	}
	platform, err := parsePlatform(string(out))
	if err != nil {
		return Platform{}, &Error{Host: host, Err: err}
	}
	return platform, nil
}

func parsePlatform(uname string) (Platform, error) {
	fields := strings.Fields(cleanText(uname))
	if len(fields) != 2 {
		return Platform{}, fmt.Errorf("unreadable answer to uname: %q", strings.Join(fields, " "))
	}
	system, machine := fields[0], fields[1]
	var platform Platform
	switch upper := strings.ToUpper(system); {
	case system == "Linux":
		platform.OS = "linux"
	case system == "Darwin":
		platform.OS = "darwin"
	case strings.HasPrefix(upper, "MINGW"), strings.HasPrefix(upper, "MSYS"), strings.HasPrefix(upper, "CYGWIN"), strings.Contains(upper, "WINDOWS"):
		return Platform{}, errWindowsHost
	default:
		return Platform{}, fmt.Errorf("agent-manager has no build for %s", system)
	}
	switch machine {
	case "x86_64", "amd64":
		platform.Arch = "amd64"
	case "arm64", "aarch64":
		platform.Arch = "arm64"
	default:
		return Platform{}, fmt.Errorf("agent-manager has no build for %s on %s", system, machine)
	}
	return platform, nil
}

// Installed is where an install left agent-manager on a host.
type Installed struct {
	Version string `json:"version"`
	Platform
	Path string `json:"path"`
	// Found is what the host's login shell runs as agent-manager; empty
	// when PATH finds none.
	Found  string `json:"found,omitempty"`
	OnPath bool   `json:"on_path"`
}

// PathAdvice names what the user adds to the host's login profile when
// its shell would not run the installed copy; the install never edits a
// profile itself.
func (i Installed) PathAdvice() string {
	dir := path.Dir(i.Path)
	switch {
	case i.OnPath:
		return ""
	case i.Found == "":
		return "add " + dir + " to PATH in the login profile there"
	default:
		return "the login shell there runs " + i.Found + " first; put " + dir + " ahead of it on PATH, or upgrade that copy"
	}
}

// uploadScript writes stdin beside the target, checks it against the
// checksum computed here, and only then renames it over the target. Its
// last three lines say where it went and whether PATH finds that copy.
const uploadScript = `set -eu
dir="$HOME/$2"
mkdir -p "$dir"
tmp=$(mktemp "$dir/.agent-manager.XXXXXX")
trap 'rm -f "$tmp"' EXIT
trap 'exit 1' HUP INT TERM
cat >"$tmp"
if command -v sha256sum >/dev/null 2>&1; then
	sum=$(sha256sum "$tmp")
elif command -v shasum >/dev/null 2>&1; then
	sum=$(shasum -a 256 "$tmp")
else
	echo "found neither sha256sum nor shasum there to check the upload; nothing was installed" >&2
	exit 1
fi
if [ "${sum%% *}" != "$1" ]; then
	echo "the upload did not arrive whole; nothing was installed" >&2
	exit 1
fi
chmod 755 "$tmp"
if ! "$tmp" --version >/dev/null 2>&1; then
	echo "the uploaded build does not run there; nothing was installed" >&2
	exit 1
fi
mv -f "$tmp" "$dir/agent-manager"
found=$(command -v agent-manager || true)
printf '%s\n%s\n' "$dir/agent-manager" "$found"
if [ -n "$found" ] && [ "$found" -ef "$dir/agent-manager" ]; then echo yes; else echo no; fi
`

// Install puts the release of version on host, in ~/.local/bin. The build
// is downloaded and verified here and streamed over SSH, so the host needs
// no network of its own, and the host checks what arrived before it
// replaces anything. Nothing runs with sudo, and no login profile is
// edited.
func (c *Client) Install(ctx context.Context, host, version string) (Installed, error) {
	tag, err := InstallTag(version)
	if err != nil {
		return Installed{}, &Error{Host: host, Err: err}
	}
	platform, err := c.Platform(ctx, host)
	if err != nil {
		return Installed{}, err
	}
	staging, err := os.MkdirTemp("", "agent-manager-host-*")
	if err != nil {
		return Installed{}, &Error{Host: host, Err: err}
	}
	defer os.RemoveAll(staging)
	binary, err := c.fetch(ctx, tag, platform.OS, platform.Arch, staging)
	if err != nil {
		return Installed{}, &Error{Host: host, Err: fmt.Errorf("download %s for %s/%s: %w", tag, platform.OS, platform.Arch, err)}
	}
	file, err := os.Open(binary)
	if err != nil {
		return Installed{}, &Error{Host: host, Err: err}
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return Installed{}, &Error{Host: host, Err: err}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Installed{}, &Error{Host: host, Err: err}
	}
	words := []string{"sh", "-c", uploadScript, "sh", hex.EncodeToString(hash.Sum(nil)), installDir}
	out, err := c.exec(ctx, host, installTimeout, file, words)
	if err != nil {
		return Installed{}, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 3 {
		return Installed{}, unreadable(host, "the install", errors.New("too few lines"))
	}
	lines = lines[len(lines)-3:]
	return Installed{
		Version:  tag,
		Platform: platform,
		Path:     cleanText(lines[0]),
		Found:    cleanText(lines[1]),
		OnPath:   lines[2] == "yes",
	}, nil
}
