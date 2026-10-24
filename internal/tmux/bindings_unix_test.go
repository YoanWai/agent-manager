//go:build !windows

package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// attachedClient attaches a real client to session id from a pane of a
// second tmux server, the way a terminal (or ssh -t) holds one, and
// returns a way to type keys into it and whether it is still attached.
func attachedClient(t *testing.T, driver *Driver, id string) (press func(key string), attached func() bool) {
	t.Helper()
	outer := testSocket + "outer"
	run := func(args ...string) *exec.Cmd {
		cmd := exec.Command(Binary, append([]string{"-L", outer}, args...)...)
		cmd.Env = commandEnv()
		return cmd
	}
	t.Cleanup(func() { run("kill-server").Run() })
	attach := ShellQuote(driver.bin) + " -L " + testSocket + " attach-session -t " + sessionName(id)
	if out, err := run("new-session", "-d", "-s", "term", "-x", "100", "-y", "30", "env -u TMUX "+attach+"; sleep 300").CombinedOutput(); err != nil {
		t.Fatalf("outer terminal: %v: %s", err, out)
	}
	attached = func() bool {
		out, _ := tmuxCmd("list-clients", "-t", sessionName(id)).Output()
		return strings.TrimSpace(string(out)) != ""
	}
	waitFor(t, "the client to attach", attached)
	return func(key string) { run("send-keys", "-t", "term", key).Run() }, attached
}

func waitFor(t *testing.T, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// promptHeadStub stands in for the binary's prompt-head check: it records
// the session name tmux passed and answers with the status in answer.
func promptHeadStub(t *testing.T) (check, answer, asked string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "it's here")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	answer, asked = filepath.Join(dir, "answer"), filepath.Join(dir, "asked")
	script := "#!/bin/sh\nprintf '%s' \"$2\" > " + ShellQuote(asked) + "\nexit \"$(cat " + ShellQuote(answer) + ")\"\n"
	stub := filepath.Join(dir, "check")
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return ShellQuote(stub) + " prompt-head", answer, asked
}

// Issue 850: an attached client, which is what focus on a remote row
// opens, gets ← back to the list at the head of the prompt the way the
// embedded focus view does, and keeps ← for the agent anywhere else.
func TestLeftAtThePromptHeadDetachesAnAttachedClient(t *testing.T) {
	driver := requireTmux(t)
	check, answer, asked := promptHeadStub(t)
	driver.SetPromptHeadCheck(check)
	t.Cleanup(func() {
		driver.SetPromptHeadCheck("")
		driver.EnsureBindings()
	})
	id := "lefthead" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "cat -v", nil, 100, 30); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	press, attached := attachedClient(t, driver, id)

	os.WriteFile(answer, []byte("1"), 0o644)
	press("Left")
	waitFor(t, "← to reach the agent", func() bool {
		out, _ := driver.capturePlain(PaneTarget(id))
		return strings.Contains(out, "^[[D") || strings.Contains(out, "^[OD")
	})
	if !attached() {
		t.Fatal("← left the session while the check said the caret was inside the prompt")
	}
	if got, _ := os.ReadFile(asked); string(got) != sessionName(id) {
		t.Fatalf("the check was asked about %q, want %q", got, sessionName(id))
	}

	os.WriteFile(answer, []byte("0"), 0o644)
	press("Left")
	waitFor(t, "← at the prompt head to detach", func() bool { return !attached() })
}

// With the arrow step off nothing binds ←, and the footer does not name it.
func TestLeftStaysTheAgentsWithTheArrowStepOff(t *testing.T) {
	driver := requireTmux(t)
	check, _, _ := promptHeadStub(t)
	driver.SetPromptHeadCheck(check)
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	if _, bound := ownedRootLines(t, tmuxCmd)["Left"]; !bound {
		t.Fatal("the arrow step on left ← unbound")
	}
	driver.SetPromptHeadCheck("")
	if err := driver.EnsureBindings(); err != nil {
		t.Fatalf("EnsureBindings: %v", err)
	}
	if line, bound := ownedRootLines(t, tmuxCmd)["Left"]; bound {
		t.Fatalf("the arrow step off left ← bound: %q", line)
	}
	id := "leftoff" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, paneDir(), "", nil, 0, 0); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })
	right, err := tmuxCmd("display-message", "-p", "-t", sessionName(id), "#{T:status-right}").CombinedOutput()
	if err != nil {
		t.Fatalf("status-right: %v", err)
	}
	if strings.Contains(string(right), "←") {
		t.Fatalf("the footer names ← with the arrow step off: %q", right)
	}
}
