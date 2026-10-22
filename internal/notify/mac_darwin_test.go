//go:build darwin

package notify

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestInstallHelperSound(t *testing.T) {
	for _, kind := range []Kind{Waiting, Finished, Errored} {
		detail, _ := describe(kind)
		t.Run(detail.macSound, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			name, err := installHelperSound(detail.macSound)
			if err != nil {
				t.Fatal(err)
			}
			if want := "agent-manager-" + detail.macSound + ".aiff"; name != want {
				t.Fatalf("sound name = %q, want %q", name, want)
			}
			path := filepath.Join(home, "Library", "Sounds", name)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(filepath.Join("/System/Library/Sounds", detail.macSound+".aiff"))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("installed sound differs from the system sound")
			}
			installed := time.Unix(1000, 0)
			if err := os.Chtimes(path, installed, installed); err != nil {
				t.Fatal(err)
			}
			if reused, err := installHelperSound(detail.macSound); err != nil || reused != name {
				t.Fatalf("reuse sound = %q, %v", reused, err)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !info.ModTime().Equal(installed) {
				t.Fatal("existing sound was rewritten")
			}
		})
	}
}

func TestInstallHelperSoundWriteFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.WriteFile(filepath.Join(home, "Library"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installHelperSound("Funk"); err == nil {
		t.Fatal("install succeeded with a file blocking the sound directory")
	}
}

// The bundle built by the live test holds a copy of this test binary, and
// a clicked banner relaunches that copy with no arguments. Handing it to
// HelperMain here is what lets the click path run in the real helper.
func TestMain(m *testing.M) {
	if LaunchedAsHelper() {
		os.Exit(HelperMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// Needs a logged-in Mac and, on first run, the user allowing "Agent
// Manager" in the permission prompt, so it only runs when asked for.
func TestHelperPostsLiveBanner(t *testing.T) {
	if os.Getenv("AM_NOTIFY_LIVE") == "" {
		t.Skip("set AM_NOTIFY_LIVE=1 to post a real banner")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	// Notification Center refuses bundles under the temp directory, so the
	// live bundle sits beside the real one under Application Support.
	dir := filepath.Join(base, "agent-manager-live-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer restore()()
	configDir = func() (string, error) { return dir, nil }
	for _, kind := range []Kind{Waiting, Finished, Errored} {
		detail, _ := describe(kind)
		t.Run(detail.macSound, func(t *testing.T) {
			if err := postThroughHelper("live-session", "live-test · codex", detail.body, detail.macSound); err != nil {
				t.Fatalf("post through helper: %v", err)
			}
		})
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if source, err = filepath.EvalSymlinks(source); err != nil {
		t.Fatal(err)
	}
	helperPath, err := materializeHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(helperPath)))
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "Resources", "source")); err != nil {
		t.Fatalf("bundle stamp missing: %v", err)
	}
	if err := exec.Command("codesign", "--verify", "--strict", bundle).Run(); err != nil {
		t.Fatalf("the bundle the manager posts through is not sealed: %v", err)
	}
	entries, err := os.ReadDir(helperHome(dir, source))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".staging.") {
			t.Fatalf("staging left behind: %s", entry.Name())
		}
	}
}

// A build is named for the binary in it, so an upgrade publishes beside
// the running one instead of replacing the executable under it.
func TestUpgradePublishesBesideTheRunningBuild(t *testing.T) {
	if os.Getenv("AM_NOTIFY_LIVE") == "" {
		t.Skip("set AM_NOTIFY_LIVE=1 to build a real signed bundle")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "agent-manager-live-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer restore()()
	configDir = func() (string, error) { return dir, nil }
	running, err := materializeHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if source, err = filepath.EvalSymlinks(source); err != nil {
		t.Fatal(err)
	}
	// An in-place upgrade is a new mtime on the same path, which is what
	// the build is named after.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(source, later, later); err != nil {
		t.Fatal(err)
	}
	upgraded, err := materializeHelper(dir)
	if err != nil {
		t.Fatal(err)
	}
	if upgraded == running {
		t.Fatal("the upgrade reused the path the running manager launches from")
	}
	if _, err := os.Stat(running); err != nil {
		t.Fatalf("the running build was taken away: %v", err)
	}
}

// Concurrent materialization inside one process agrees on one bundle.
// What keeps a second process from taking that bundle apart is the path
// keying, which no test here covers: it needs a second manager.
func TestConcurrentMaterializeAgreesOnOneBundle(t *testing.T) {
	if os.Getenv("AM_NOTIFY_LIVE") == "" {
		t.Skip("set AM_NOTIFY_LIVE=1 to build a real signed bundle")
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "agent-manager-live-test")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer restore()()
	configDir = func() (string, error) { return dir, nil }
	var wg sync.WaitGroup
	paths := make([]string, 4)
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path, err := materializeHelper(dir)
			if err != nil {
				t.Error(err)
				return
			}
			paths[i] = path
		}(i)
	}
	wg.Wait()
	for _, path := range paths {
		if path != paths[0] {
			t.Fatalf("materialize returned %q and %q", path, paths[0])
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("the returned helper is not there: %v", err)
		}
	}
}
