//go:build !windows

package sessioncmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/conversation"
)

// Hermes names a /branch or /resume only in the crumb it writes as it
// closes, so a revive reads it once the shell that started the agent has
// marked it ended.
func TestSettleConversationReadsHermesOnceItHasClosed(t *testing.T) {
	st, manager, sess, hermesHome := hermesRow(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	agentFile := manager.AgentFile(sess.ID)
	shell := exec.Command("sh", "-c", `sh -c "echo \$\$ > `+pidFile+`; exec sleep 60"; `+conversation.EndedCommand(agentFile))
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		shell.Wait()
		close(exited)
	}()
	agentPID := readPIDFile(t, pidFile)
	t.Cleanup(func() {
		syscall.Kill(agentPID, syscall.SIGKILL)
		<-exited
	})
	if err := os.WriteFile(agentFile, []byte(strconv.Itoa(agentPID)+" 0 /dev/ttys029\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeHermesCrumb(t, hermesHome, sess.Cwd, "20261008_005341_0d388a", time.Now())

	settled, err := SettleConversation(st, manager, sess, "hermes", false, 0)
	if err != nil || settled.AgentSessionID != "20261008_005433_0c8868" {
		t.Fatalf("while the agent runs = %q, %v; want the row left alone", settled.AgentSessionID, err)
	}
	if settled, err := SettleConversation(st, manager, sess, "grok", false, 0); err != nil || settled.AgentSessionID != sess.AgentSessionID {
		t.Fatalf("another style = %q, %v", settled.AgentSessionID, err)
	}
	go func() {
		time.Sleep(200 * time.Millisecond)
		syscall.Kill(agentPID, syscall.SIGKILL)
	}()
	settled, err = SettleConversation(st, manager, sess, "hermes", false, closingAgentWait)
	if err != nil || settled.AgentSessionID != "20261008_005341_0d388a" || storedConversation(t, st, "abcd1234") != "20261008_005341_0d388a" {
		t.Fatalf("once closed = %q, %v; want the conversation it closed on", settled.AgentSessionID, err)
	}
}
func readPIDFile(t *testing.T, path string) int {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		data, _ := os.ReadFile(path)
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			return pid
		}
	}
	t.Fatalf("no pid in %s", path)
	return 0
}
