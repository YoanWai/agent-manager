//go:build !darwin && !linux && !windows

package store

import "fmt"

func tryDeliveryFileLock(path string) (func(), bool, error) {
	return nil, false, fmt.Errorf("automatic delivery ownership is unsupported on this platform")
}
