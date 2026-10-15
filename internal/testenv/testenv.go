// Package testenv holds test helpers shared across packages.
package testenv

import (
	"runtime"
	"testing"
)

// SkipPOSIXShell skips a test whose fixture is a POSIX shell script, which
// Windows has no shell to run.
func SkipPOSIXShell(t testing.TB) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fixture is a POSIX shell script")
	}
}
