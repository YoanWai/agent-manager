//go:build !windows

package ui

// rawConsole has nothing to do here: the fixtures only stand in for cat and
// sh on Windows, and a tty already runs the line discipline they emulate.
func rawConsole() func() { return func() {} }

func shortPath(path string) string { return path }
