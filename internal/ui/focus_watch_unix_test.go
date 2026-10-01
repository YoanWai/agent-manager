//go:build darwin || linux

package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFocusWatchCloseCancelsAndReapsAnUngreetedControl(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "control-pid")
	stub := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *" has-session "*) exit 0 ;;
  *" attach-session "*)
    echo $$ > %s
    while :; do sleep 1; done
    ;;
esac
exit 0
`, tmux.ShellQuote(pidPath))
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_TMPDIR", dir)
	driver, err := tmux.NewWithSocket("focus-watch-cancel")
	if err != nil {
		t.Fatal(err)
	}
	watch := newFocusWatch(driver, func(tea.Msg) {})
	watch.setFocus("fixture")
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(pidPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			watch.Close()
			t.Fatal("control attach never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	started := time.Now()
	watch.Close()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("focus watch close took %v", elapsed)
	}
	rawPID, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("control process %d still exists after watcher close: %v", pid, err)
	}
}

func TestFocusWatchCloseReapsControlWhileSendCallbackIsBlocked(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "control-pid")
	stub := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *" has-session "*) exit 0 ;;
  *" attach-session "*)
    echo $$ > %s
    printf '%%%%begin 1 1 0\n%%%%end 1 1 0\n'
    block=2
    while IFS= read -r command; do
      printf '%%%%begin 1 %%s 0\n' "$block"
      case "$command" in
        capture-pane*) printf 'fixture frame\n' ;;
        display-message*) printf '0,0,1,000,0,0,0\n' ;;
      esac
      printf '%%%%end 1 %%s 0\n' "$block"
      block=$((block + 1))
    done
    ;;
esac
exit 0
`, tmux.ShellQuote(pidPath))
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX_TMPDIR", dir)
	driver, err := tmux.NewWithSocket("focus-watch-blocked-send")
	if err != nil {
		t.Fatal(err)
	}
	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	var startOnce, releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCallback) }) }
	watch := newFocusWatch(driver, func(msg tea.Msg) {
		if _, ok := msg.(focusPreviewMsg); !ok {
			return
		}
		startOnce.Do(func() { close(callbackStarted) })
		<-releaseCallback
	})
	t.Cleanup(func() {
		release()
		watch.Close()
	})
	watch.setFocus("fixture")
	select {
	case <-callbackStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("watcher never reached the blocked send callback")
	}

	closeCtx, cancelClose := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err = watch.CloseContext(closeCtx)
	cancelClose()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CloseContext error = %v, want callback deadline", err)
	}
	rawPID, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		err = syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("control process %d survived canceled watcher with blocked callback: %v", pid, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
}
