//go:build !windows

package ui

// paneShellWarning has nothing to warn about: panes run the user's POSIX
// shell, which passes arguments to the agent CLI as written.
func paneShellWarning() string {
	return ""
}
