//go:build darwin

package notify

import (
	"os"
	"path/filepath"
	"testing"
)

// The bundle built by the live test holds a copy of this test binary, and
// a clicked banner relaunches that copy with no arguments. Handing it to
// HelperMain here is what lets the click path run in the real helper.
func TestMain(m *testing.M) {
	if LaunchedAsHelper() {
		os.Exit(HelperMain(os.Args[1:]))
	}
	os.Exit(m.Run())
}

// TestHelperPostsLiveBanner builds the notifier bundle from this test
// binary and posts one banner through Notification Center. It needs a
// logged-in Mac and, on first run, the user allowing "Agent Manager" in
// the permission prompt, so it only runs when asked for by name.
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
	if err := postThroughHelper("live-session", "live-test · codex", "● Finished", "Hero"); err != nil {
		t.Fatalf("post through helper: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, helperBundle, "Contents", "Resources", "source")); err != nil {
		t.Fatalf("bundle stamp missing: %v", err)
	}
}
