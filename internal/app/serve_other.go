//go:build !unix

package app

import (
	"fmt"
	"runtime"
)

func StartDetached(args []string, logPath string) (int, error) {
	return 0, fmt.Errorf("serve --background cannot detach a manager on %s", runtime.GOOS)
}
