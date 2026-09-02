//go:build !darwin

package notify

import "errors"

func postThroughHelper(sessionID, subtitle, body, sound string) error {
	return errors.New("notification helper is macOS only")
}

// LaunchedAsHelper is false everywhere but macOS, where the notifier
// bundle runs a copy of this binary.
func LaunchedAsHelper() bool { return false }

// HelperMain has no work outside macOS.
func HelperMain(args []string) int { return 1 }
