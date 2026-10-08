//go:build darwin

package sysstat

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

type diskFoundation struct {
	createURL    func(uintptr, string, int64, bool) uintptr
	copyProperty func(uintptr, uintptr, *uintptr, uintptr) bool
	numberValue  func(uintptr, int64, *int64) bool
	release      func(uintptr)
	totalKey     uintptr
	availableKey uintptr
}

var loadDiskFoundation = sync.OnceValues(func() (*diskFoundation, error) {
	lib, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	f := &diskFoundation{}
	purego.RegisterLibFunc(&f.createURL, lib, "CFURLCreateFromFileSystemRepresentation")
	purego.RegisterLibFunc(&f.copyProperty, lib, "CFURLCopyResourcePropertyForKey")
	purego.RegisterLibFunc(&f.numberValue, lib, "CFNumberGetValue")
	purego.RegisterLibFunc(&f.release, lib, "CFRelease")
	var copyPointer func(*uintptr, uintptr, uintptr) uintptr
	purego.RegisterLibFunc(&copyPointer, lib, "memcpy")
	for name, key := range map[string]*uintptr{
		"kCFURLVolumeTotalCapacityKey":                      &f.totalKey,
		"kCFURLVolumeAvailableCapacityForImportantUsageKey": &f.availableKey,
	} {
		addr, err := purego.Dlsym(lib, name)
		if err != nil {
			return nil, err
		}
		copyPointer(key, addr, unsafe.Sizeof(*key))
	}
	return f, nil
})

func sampleDisk(snap *Snapshot, diskPath string) {
	if diskPath == "" {
		diskPath = "/"
	}
	total, available, err := readDarwinDisk(diskPath)
	if err != nil {
		sampleDiskFallback(snap, diskPath)
		return
	}
	snap.DiskTotal = total
	snap.DiskAvailable = available
	snap.DiskUsed = total - available
	snap.DiskPercent = usedPercent(snap.DiskUsed, total)
	snap.DiskOK = true
}

func readDarwinDisk(path string) (uint64, uint64, error) {
	f, err := loadDiskFoundation()
	if err != nil {
		return 0, 0, err
	}
	url := f.createURL(0, path, int64(len(path)), false)
	if url == 0 {
		return 0, 0, fmt.Errorf("invalid disk path %q", path)
	}
	defer f.release(url)
	total, err := f.capacity(url, f.totalKey)
	if err != nil {
		return 0, 0, err
	}
	// Important capacity includes caches macOS can purge when space is needed.
	available, err := f.capacity(url, f.availableKey)
	if err != nil {
		return 0, 0, err
	}
	if total == 0 || available > total {
		return 0, 0, fmt.Errorf("invalid disk capacity %d available of %d", available, total)
	}
	return total, available, nil
}

func (f *diskFoundation) capacity(url, key uintptr) (uint64, error) {
	var number uintptr
	if !f.copyProperty(url, key, &number, 0) || number == 0 {
		return 0, fmt.Errorf("disk capacity unavailable")
	}
	defer f.release(number)
	var value int64
	const cfNumberSInt64 = 4
	if !f.numberValue(number, cfNumberSInt64, &value) || value < 0 {
		return 0, fmt.Errorf("invalid disk capacity")
	}
	return uint64(value), nil
}
