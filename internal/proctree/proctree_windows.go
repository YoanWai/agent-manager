//go:build windows

package proctree

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Entry is one process: its id, its parent's id, and its executable name
// lowercased without the .exe suffix, the way ps -o comm names it.
type Entry struct {
	PID, PPID int
	Name      string
}

// Snapshot lists every process on the machine at one instant.
func Snapshot() ([]Entry, error) {
	handle, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	defer windows.CloseHandle(handle)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	var entries []Entry
	for err = windows.Process32First(handle, &entry); err == nil; err = windows.Process32Next(handle, &entry) {
		name := strings.ToLower(windows.UTF16ToString(entry.ExeFile[:]))
		entries = append(entries, Entry{
			PID:  int(entry.ProcessID),
			PPID: int(entry.ParentProcessID),
			Name: strings.TrimSuffix(name, ".exe"),
		})
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	return entries, nil
}
