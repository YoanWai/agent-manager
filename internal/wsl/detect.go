// Package wsl reports whether the process runs under WSL.
package wsl

import (
	"os"
	"strings"
)

var osReleaseFile = "/proc/sys/kernel/osrelease"

func Detect() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile(osReleaseFile)
	if err != nil {
		return false
	}
	release := strings.ToLower(string(data))
	return strings.Contains(release, "microsoft") || strings.Contains(release, "wsl")
}
