// Package testenv holds test helpers shared across packages.
package testenv

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
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

// IsolateGit unsets the variables that bind git to one repository, as a
// hook or `git rebase --exec` exports them. Inherited, they point every git
// command a test runs in a temp dir at the developer's own repo instead.
// git names the variables itself, so a release that adds one is covered.
func IsolateGit() error {
	out, err := exec.Command("git", "rev-parse", "--local-env-vars").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("git rev-parse --local-env-vars: %w", err)
	}
	for _, key := range strings.Fields(string(out)) {
		os.Unsetenv(key)
	}
	return nil
}
