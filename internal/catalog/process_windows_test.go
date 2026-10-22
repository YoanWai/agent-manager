//go:build windows

package catalog

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// A manager killed outright never reaches StopAll; the job it held closes
// with it and ends the server and the child the server started.
func TestAManagerThatDiesTakesItsCatalogServersWithIt(t *testing.T) {
	dir := t.TempDir()
	serverFile, childFile := filepath.Join(dir, "server.pid"), filepath.Join(dir, "child.pid")
	host := exec.Command(os.Args[0])
	host.Env = append(os.Environ(), "CATALOG_FAKE=catalog-host", "CATALOG_SERVER_PID="+serverFile, "CATALOG_CHILD_PID="+childFile)
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	server, child := waitPid(t, serverFile), waitPid(t, childFile)
	if err := host.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = host.Wait()
	for deadline := time.Now().Add(2 * time.Second); processAlive(server) || processAlive(child); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("server %d alive %v, child %d alive %v after the manager died", server, processAlive(server), child, processAlive(child))
		}
	}
}

// processAlive reports whether a pid still has a running process behind it.
// The handle signals once the process ends; an exit code would not tell a
// process that exited with 259 from one still running.
func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	event, err := windows.WaitForSingleObject(handle, 0)
	return err != nil || event != windows.WAIT_OBJECT_0
}
