//go:build windows

package catalog

// processAlive: Windows has no signal-0 probe, and a test cannot observe the
// exit of a process it did not start with a handle to, so the liveness
// check trusts the stdin-close stop the process watches.
func processAlive(int) bool {
	return false
}
