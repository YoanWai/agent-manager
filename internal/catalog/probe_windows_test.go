//go:build windows

package catalog

import "golang.org/x/sys/windows"

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
