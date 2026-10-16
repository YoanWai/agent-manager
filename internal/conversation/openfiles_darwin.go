package conversation

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// libproc's flavors and record sizes, from <sys/proc_info.h>.
const (
	procPIDListFDs         = 1
	proxFDTypeVnode        = 1
	procPIDFDVnodePathInfo = 2
	fdInfoSize             = 8
	vnodePathInfoSize      = 1200
	maxPathLen             = 1024
)

type procFDInfo struct {
	fd     int32
	fdType uint32
}

var (
	libprocOnce   sync.Once
	libprocErr    error
	procPIDInfo   func(pid, flavor int32, arg uint64, buffer unsafe.Pointer, size int32) int32
	procPIDFDInfo func(pid, fd, flavor int32, buffer unsafe.Pointer, size int32) int32
)

func loadLibproc() error {
	libprocOnce.Do(func() {
		lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			libprocErr = err
			return
		}
		for name, fn := range map[string]any{"proc_pidinfo": &procPIDInfo, "proc_pidfdinfo": &procPIDFDInfo} {
			symbol, err := purego.Dlsym(lib, name)
			if err != nil {
				libprocErr = err
				return
			}
			purego.RegisterFunc(fn, symbol)
		}
	})
	return libprocErr
}

// openFiles lists the paths of the files and directories pid holds open,
// through libproc, which answers for a single process in well under a
// millisecond where lsof takes tens.
func openFiles(pid int) ([]string, error) {
	if err := loadLibproc(); err != nil {
		return nil, err
	}
	needed := procPIDInfo(int32(pid), procPIDListFDs, 0, nil, 0)
	if needed <= 0 {
		if !Alive(pid) {
			return nil, nil
		}
		return nil, fmt.Errorf("list the files process %d holds open: proc_pidinfo failed", pid)
	}
	fds := make([]procFDInfo, needed/fdInfoSize+16)
	filled := procPIDInfo(int32(pid), procPIDListFDs, 0, unsafe.Pointer(&fds[0]), int32(len(fds)*fdInfoSize))
	if filled <= 0 {
		return nil, nil
	}
	var paths []string
	info := make([]byte, vnodePathInfoSize)
	for _, fd := range fds[:filled/fdInfoSize] {
		if fd.fdType != proxFDTypeVnode {
			continue
		}
		if procPIDFDInfo(int32(pid), fd.fd, procPIDFDVnodePathInfo, unsafe.Pointer(&info[0]), vnodePathInfoSize) != vnodePathInfoSize {
			continue
		}
		path := info[vnodePathInfoSize-maxPathLen:]
		for i, b := range path {
			if b == 0 {
				path = path[:i]
				break
			}
		}
		paths = append(paths, string(path))
	}
	return paths, nil
}
