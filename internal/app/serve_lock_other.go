//go:build !unix

package app

import (
	"fmt"
	"runtime"
)

func tryServeLock(string) (func(), bool, error) {
	return nil, false, fmt.Errorf("serve cannot lock its profile on %s", runtime.GOOS)
}
