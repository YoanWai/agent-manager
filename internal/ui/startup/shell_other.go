//go:build !windows

package startup

// PaneShellWarning has nothing to warn about: panes run the user's POSIX
// shell, which passes arguments to the agent CLI as written.
func PaneShellWarning() string { return "" }
