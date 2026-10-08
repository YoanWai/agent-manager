package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A launcher such as a Volta shim spawns the CLI as its child, and gemini
// relaunches itself in a child of its own, all in the launched process's
// group. Commands the agent runs sit further down or in a group of their own.
// This test process stands in for the launched one.
func TestASpawnedAgentRunsInTheLaunchedGroupWithinTwoLevels(t *testing.T) {
	start := func(command *exec.Cmd) int {
		t.Helper()
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			command.Process.Kill()
			command.Wait()
		})
		return command.Process.Pid
	}
	startBelow := func(script, pidFile string) int {
		t.Helper()
		start(exec.Command("sh", "-c", script))
		pid := readPIDFile(t, pidFile)
		t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })
		return pid
	}
	ownGroup := exec.Command("sleep", "63")
	ownGroup.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	gone := exec.Command("true")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	grandchildFile, greatGrandchildFile := filepath.Join(dir, "grandchild"), filepath.Join(dir, "great-grandchild")
	pids := map[string]int{
		"launched":         os.Getpid(),
		"child":            start(exec.Command("sleep", "61")),
		"own-group":        start(ownGroup),
		"grandchild":       startBelow("sleep 62 & echo $! > "+grandchildFile+"; wait", grandchildFile),
		"great-grandchild": startBelow(`sh -c "sleep 64 & echo \$! > `+greatGrandchildFile+`; wait"; true`, greatGrandchildFile),
		"gone":             gone.Process.Pid,
	}
	for _, tc := range []struct {
		reporter string
		spawned  bool
		want     bool
	}{
		{"launched", false, true},
		{"launched", true, true},
		{"child", false, false},
		{"child", true, true},
		{"grandchild", false, false},
		{"grandchild", true, true},
		{"great-grandchild", true, false},
		{"own-group", true, false},
		{"gone", true, false},
	} {
		if got, err := FromAgent(os.Getpid(), pids[tc.reporter], tc.spawned); err != nil || got != tc.want {
			t.Errorf("%s with spawned %v = %v, %v; want %v", tc.reporter, tc.spawned, got, err, tc.want)
		}
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
