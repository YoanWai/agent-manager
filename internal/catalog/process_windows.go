//go:build windows

package catalog

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree is a job object holding a server and every process it starts. The
// job kills them all when its last handle closes, so they end with the
// manager however it exits, a crash included.
type tree windows.Handle

// prepareTree starts the server suspended, so it cannot start a child
// before adoptTree puts it in its job.
func prepareTree(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
}

// adoptTree puts the suspended server in a job of its own and lets it run.
func adoptTree(cmd *exec.Cmd) (tree, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, fmt.Errorf("catalog job: %w", err)
	}
	if err := joinJob(job, uint32(cmd.Process.Pid)); err != nil {
		windows.CloseHandle(job)
		return 0, fmt.Errorf("catalog job: %w", err)
	}
	return tree(job), nil
}

func joinJob(job windows.Handle, pid uint32) error {
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		return err
	}
	return resumeProcess(pid)
}

// resumeProcess resumes the threads of a process started suspended. Go
// closes the primary thread's handle once the process is created, so the
// thread is found again by its owner.
func resumeProcess(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	resumed := false
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID != pid {
			continue
		}
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, entry.ThreadID)
		if err != nil {
			return err
		}
		_, err = windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return err
		}
		resumed = true
	}
	if !resumed {
		return fmt.Errorf("no thread of process %d to resume", pid)
	}
	return nil
}

// term has no signal to send on Windows; the stdin stop closes is the
// polite ask.
func (tree) term() {}

// close releases the job, which ends whatever of the tree is left: the
// server or a child that outlived it.
func (t tree) close() {
	_ = windows.CloseHandle(windows.Handle(t))
}
