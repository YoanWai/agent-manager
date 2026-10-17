//go:build darwin || linux

package tmux

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A history capture its caller gave up on returns at once, instead of
// holding a quote pass or a reply copy until the tmux child gives up.
func TestCapturePaneHistoryContextStopsWithItsContext(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "blocked-tmux")
	if err := os.WriteFile(blocked, []byte("#!/bin/sh\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	defer cancel()

	started := time.Now()
	_, err := (&Driver{bin: blocked, socket: testSocket}).CapturePaneHistoryContext(ctx, "x1", 300)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("CapturePaneHistoryContext error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("capture outlived its cancelled context by %v", elapsed)
	}
}
