//go:build !windows

package config

// checkBatchShim has nothing to refuse where no cmd.exe parses arguments.
func checkBatchShim(string, string) error {
	return nil
}
