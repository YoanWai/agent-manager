package remote

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParsePlatformMapsUnameToGoNames(t *testing.T) {
	for _, tc := range []struct {
		uname string
		want  Platform
	}{
		{"Linux x86_64\n", Platform{OS: "linux", Arch: "amd64"}},
		{"Linux aarch64", Platform{OS: "linux", Arch: "arm64"}},
		{"Darwin arm64", Platform{OS: "darwin", Arch: "arm64"}},
		{"Darwin x86_64", Platform{OS: "darwin", Arch: "amd64"}},
	} {
		got, err := parsePlatform(tc.uname)
		if err != nil || got != tc.want {
			t.Errorf("parsePlatform(%q) = %+v, %v; want %+v", tc.uname, got, err, tc.want)
		}
	}
}

func TestParsePlatformRefusesWhatNoReleaseBuilds(t *testing.T) {
	for _, tc := range []struct {
		uname   string
		windows bool
	}{
		{"MINGW64_NT-10.0-19045 x86_64", true},
		{"MSYS_NT-10.0 x86_64", true},
		{"CYGWIN_NT-10.0 x86_64", true},
		{"FreeBSD amd64", false},
		{"Linux riscv64", false},
		{"Linux", false},
	} {
		_, err := parsePlatform(tc.uname)
		if err == nil {
			t.Errorf("parsePlatform(%q) succeeded", tc.uname)
			continue
		}
		if errors.Is(err, errWindowsHost) != tc.windows {
			t.Errorf("parsePlatform(%q) = %v; Windows refusal %v", tc.uname, err, tc.windows)
		}
	}
}

func TestInstallRefusesADevBuildBeforeAnyCall(t *testing.T) {
	calls, fetches := 0, 0
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		calls++
		return nil, nil, nil
	}, WithFetcher(func(context.Context, string, string, string, string) (string, error) {
		fetches++
		return "", nil
	}))
	for _, version := range []string{"dev", "0.40.0-rc.1", ""} {
		_, err := c.Install(context.Background(), "gpu", version)
		if err == nil || !strings.Contains(err.Error(), "is not a release") {
			t.Errorf("Install with version %q = %v, want the dev-build refusal", version, err)
		}
	}
	if calls != 0 || fetches != 0 {
		t.Fatalf("a dev build still made %d ssh calls and %d downloads", calls, fetches)
	}
}

func TestInstallRefusesAWindowsHostBeforeDownloading(t *testing.T) {
	var commands [][]string
	c := newTestClient(t, func(_ context.Context, argv []string) ([]byte, []byte, error) {
		words := remoteWords(t, argv)
		commands = append(commands, words)
		return []byte("MINGW64_NT-10.0-19045 x86_64\n"), nil, nil
	}, WithFetcher(func(context.Context, string, string, string, string) (string, error) {
		t.Fatal("a Windows host was sent a download")
		return "", nil
	}))
	_, err := c.Install(context.Background(), "gpu", "0.41.0")
	if !errors.Is(err, errWindowsHost) || !strings.HasPrefix(err.Error(), "gpu: ") {
		t.Fatalf("Install = %v, want the Windows refusal for gpu", err)
	}
	if len(commands) != 1 || commands[0][0] != "uname" {
		t.Fatalf("ran %q, want only uname", commands)
	}
}

func TestInstallTagIsThisBuildsRelease(t *testing.T) {
	for version, want := range map[string]string{"0.41.0": "v0.41.0", "v1.2.3": "v1.2.3"} {
		if got, err := InstallTag(version); err != nil || got != want {
			t.Errorf("InstallTag(%q) = %q, %v; want %q", version, got, err, want)
		}
	}
}

func TestInstallableNamesOutdatedAndMissingHostsOnly(t *testing.T) {
	if !Installable(outdated("gpu")) || !Installable(&Error{Host: "gpu", Err: errNotInstalled}) {
		t.Fatal("an outdated or missing agent-manager is not installable")
	}
	for _, err := range []error{
		&Error{Host: "gpu", Err: errors.New("unreachable over SSH: refused"), sshFailed: true},
		&Error{Host: "gpu", Err: errors.New("agent-manager there is newer than this one; update agent-manager on this machine")},
		nil,
	} {
		if Installable(err) {
			t.Errorf("Installable(%v) = true", err)
		}
	}
}
