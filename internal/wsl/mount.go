package wsl

import (
	"os"
	"strconv"
	"strings"
)

var mountInfoFile = "/proc/self/mountinfo"

// windowsMountTypes are the filesystem types a Windows drive is mounted
// into a distro with, and the only ones WSL's own path translation
// accepts: 9p and virtiofs under WSL2, drvfs under WSL1. The automount
// root is configurable and a distro can bind a drive anywhere, so the
// type is what names these mounts, not a /mnt prefix. WSL's other 9p
// mounts carry its own tools and drivers, at fixed paths no PATH entry
// sits under.
var windowsMountTypes = map[string]bool{"9p": true, "virtiofs": true, "drvfs": true}

// OnWindowsMount reports whether a path sits on a Windows drive mounted
// into this distro. The innermost mount containing the path decides, so a
// Linux filesystem mounted under a Windows drive answers false.
func OnWindowsMount(path string) (bool, error) {
	data, err := os.ReadFile(mountInfoFile)
	if err != nil {
		return false, err
	}
	innermost, windows := "", false
	for _, line := range strings.Split(string(data), "\n") {
		point, fstype, ok := mountEntry(line)
		if !ok || !underMount(path, point) {
			continue
		}
		// Mounts stack, so the last entry at a given point is the live one.
		if len(point) >= len(innermost) {
			innermost, windows = point, windowsMountTypes[fstype]
		}
	}
	return windows, nil
}

// mountEntry reads a mountinfo line's mount point and filesystem type.
// The optional fields between them are variable in number and end at a
// lone dash, which is what the two halves are cut on.
func mountEntry(line string) (point, fstype string, ok bool) {
	head, tail, found := strings.Cut(line, " - ")
	if !found {
		return "", "", false
	}
	fields := strings.Fields(head)
	rest := strings.Fields(tail)
	if len(fields) < 5 || len(rest) == 0 {
		return "", "", false
	}
	return unescapeMount(fields[4]), rest[0], true
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
