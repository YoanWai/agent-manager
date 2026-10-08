//go:build unix

package app

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func holdServeLock(t *testing.T, profileDir string) {
	t.Helper()
	file, err := os.OpenFile(filepath.Join(profileDir, ServeLock), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("hold the serve lock: %v", err)
	}
}

func TestLockServeHoldsUntilReleased(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "profile")
	release, acquired, err := LockServe(dir)
	if err != nil || !acquired {
		t.Fatalf("first LockServe = %v, %v", acquired, err)
	}
	if again, held, err := LockServe(dir); err != nil || held || again != nil {
		t.Fatalf("second LockServe while held = %v, %v", held, err)
	}
	release()
	release, acquired, err = LockServe(dir)
	if err != nil || !acquired {
		t.Fatalf("LockServe after release = %v, %v", acquired, err)
	}
	release()
}

// A serve that has not polled yet has no heartbeat, but it holds the lock,
// and a second start would run two managers on one profile.
func TestServeBackgroundLeavesAHeldServeLockAlone(t *testing.T) {
	dir := t.TempDir()
	holdServeLock(t, dir)
	starter := &recordedStart{}
	got, err := ServeBackground(dir, time.Now(), starter.start)
	if err != nil {
		t.Fatalf("ServeBackground: %v", err)
	}
	if got.Started || starter.calls != 0 {
		t.Fatalf("a held lock got a second serve: %+v, starts=%d", got, starter.calls)
	}
}
