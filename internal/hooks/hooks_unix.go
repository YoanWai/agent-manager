//go:build !windows

package hooks

import "os"

// installScriptExt names the install script sh runs.
const installScriptExt = ".install.sh"

func statusCommand(state string) string {
	return `printf ` + state + ` > "$` + EnvStatusFile + `"`
}

func sessionEndCommand() string {
	return `rm -f "$` + EnvStatusFile + `"`
}

func installScriptContent(body string) []byte {
	return []byte(body)
}

func readMailbox(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func moveMailbox(from, to string) error {
	return os.Rename(from, to)
}
