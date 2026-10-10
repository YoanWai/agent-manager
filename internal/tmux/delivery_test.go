//go:build darwin || linux

package tmux

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestSendTextContextClassifiesLoadFailureAsRefused(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls")
	stub := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
echo "$@" >> %s
case " $* " in
  *" load-buffer "*) exit 41 ;;
esac
exit 0
`, ShellQuote(logPath))
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &Driver{bin: stub, socket: testSocket}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	result, err := driver.SendTextContext(ctx, "x1", "hello")
	if err == nil {
		t.Fatal("load-buffer failure was reported as success")
	}
	if result.Phase != SendPhaseLoadStarted || result.PasteMayHaveStarted() {
		t.Fatalf("load failure result = %+v", result)
	}
	logged, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(logged), "paste-buffer") {
		t.Fatalf("paste ran after refused load:\n%s", logged)
	}
}

func TestSendTextContextKillsAndReapsTransportBeforeReturning(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	parentPID := filepath.Join(dir, "parent-pid")
	late := filepath.Join(dir, "late-child")
	stub := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
case " $* " in
  *" paste-buffer "*)
    echo $$ > %s
    : > %s
    (sleep 0.5; echo late > %s) &
    while :; do sleep 1; done
    ;;
esac
exit 0
`, ShellQuote(parentPID), ShellQuote(started), ShellQuote(late))
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &Driver{bin: stub, socket: testSocket}
	ctx, cancel := context.WithCancel(context.Background())
	type response struct {
		result SendResult
		err    error
	}
	done := make(chan response, 1)
	go func() {
		result, err := driver.SendTextContext(ctx, "x1", "hello")
		done <- response{result: result, err: err}
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("transport never reached paste-buffer")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	var got response
	select {
	case got = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled transport did not return")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("transport error = %v, want context.Canceled", got.err)
	}
	if got.result.Phase != SendPhasePasteStarted || !got.result.PasteMayHaveStarted() {
		t.Fatalf("canceled paste result = %+v", got.result)
	}

	rawPID, err := os.ReadFile(parentPID)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("transport process %d still exists after return: %v", pid, err)
	}
	time.Sleep(650 * time.Millisecond)
	if _, err := os.Stat(late); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("transport child resumed after guard-safe return: %v", err)
	}
}

func TestOpenControlContextKillsAndReapsAnUngreetedClient(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "control-pid")
	stub := filepath.Join(dir, "tmux")
	script := fmt.Sprintf(`#!/bin/sh
echo $$ > %s
while :; do sleep 1; done
`, ShellQuote(pidPath))
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	driver := &Driver{bin: stub, socket: "am-control-open-timeout"}
	ctx, cancel := context.WithCancel(context.Background())
	type response struct {
		control *Control
		err     error
	}
	done := make(chan response, 1)
	go func() {
		control, err := driver.OpenControlContext(ctx, "x1")
		done <- response{control: control, err: err}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, statErr := os.Stat(pidPath); statErr == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("control process never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	var opened response
	select {
	case opened = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled control open did not return")
	}
	control, err := opened.control, opened.err
	if control != nil {
		t.Fatal("ungreeted client returned a live control")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenControlContext error = %v, want context canceled", err)
	}
	rawPID, readErr := os.ReadFile(pidPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	pid, parseErr := strconv.Atoi(strings.TrimSpace(string(rawPID)))
	if parseErr != nil {
		t.Fatal(parseErr)
	}
	if killErr := syscall.Kill(pid, 0); !errors.Is(killErr, syscall.ESRCH) {
		t.Fatalf("control process %d still exists after open returned: %v", pid, killErr)
	}
}

func TestSessionExistsContextSeparatesAbsenceFromTransportFailure(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing-tmux")
	if err := os.WriteFile(missing, []byte("#!/bin/sh\necho \"can't find session: am_x1\" >&2\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	exists, err := (&Driver{bin: missing, socket: testSocket}).SessionExistsContext(context.Background(), "x1")
	if err != nil || exists {
		t.Fatalf("missing session = exists %v, err %v", exists, err)
	}

	blocked := filepath.Join(dir, "blocked-tmux")
	if err := os.WriteFile(blocked, []byte("#!/bin/sh\nwhile :; do sleep 1; done\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	exists, err = (&Driver{bin: blocked, socket: testSocket}).SessionExistsContext(ctx, "x1")
	if exists || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked transport = exists %v, err %v", exists, err)
	}
}
