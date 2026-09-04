package wsl

import (
	"os"
	"strconv"
	"strings"
)

var mountInfoFile = "/proc/self/mountinfo"

// OnWindowsMount reports whether a path sits on a Windows drive mounted
// into this distro. The innermost mount containing the path decides, so a
// Linux filesystem mounted under a drive answers false.
func OnWindowsMount(path string) (bool, error) {
	data, err := os.ReadFile(mountInfoFile)
	if err != nil {
		return false, err
	}
	innermost, windows := "", false
	for _, line := range strings.Split(string(data), "\n") {
		mount, ok := mountEntry(line)
		if !ok || !underMount(path, mount.point) {
			continue
		}
		// Mounts stack, so the last entry at a given point is the live one.
		if len(mount.point) >= len(innermost) {
			innermost, windows = mount.point, mount.isWindowsDrive()
		}
	}
	return windows, nil
}

type mount struct {
	point        string
	fstype       string
	superOptions string
}

// isWindowsDrive answers the question WSL's own path translation asks of a
// mount. WSL1 puts a drive on its own filesystem type. WSL2 shares one over
// Plan 9 or virtio, and serves its init binary, libraries and GPU drivers
// over Plan 9 as well, so a 9p mount is a drive only when it carries the
// drvfs attach name those others do not.
func (m mount) isWindowsDrive() bool {
	switch m.fstype {
	case "drvfs", "virtiofs":
		return true
	case "9p":
		for _, option := range strings.Split(m.superOptions, ",") {
			if strings.HasPrefix(option, "aname=drvfs") {
				return true
			}
		}
	}
	return false
}

// mountEntry reads a mountinfo line. The optional fields before the
// filesystem type are variable in number and end at a lone dash, and the
// super options after it can hold a raw space when a Windows path does,
// so the line is cut on the separator rather than counted into.
func mountEntry(line string) (mount, bool) {
	head, tail, found := strings.Cut(line, " - ")
	if !found {
		return mount{}, false
	}
	fields := strings.Fields(head)
	rest := strings.SplitN(tail, " ", 3)
	if len(fields) < 5 || len(rest) < 3 || rest[0] == "" {
		return mount{}, false
	}
	return mount{point: unescapeMount(fields[4]), fstype: rest[0], superOptions: rest[2]}, true
}

// unescapeMount decodes the octal escapes mountinfo writes for the
// characters its own format uses: space, tab, newline and backslash.
func unescapeMount(field string) string {
	if !strings.Contains(field, `\`) {
		return field
	}
	var out strings.Builder
	for i := 0; i < len(field); i++ {
		if field[i] == '\\' && i+3 < len(field) {
			if value, err := strconv.ParseUint(field[i+1:i+4], 8, 8); err == nil {
				out.WriteByte(byte(value))
				i += 3
				continue
			}
		}
		out.WriteByte(field[i])
	}
	return out.String()
}

func underMount(path, point string) bool {
	if point == "/" {
		return true
	}
	return path == point || strings.HasPrefix(path, point+"/")
}
