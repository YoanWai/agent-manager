//go:build !darwin

package notify

import "errors"

func postThroughHelper(sessionID, subtitle, body, sound string) error {
	return errors.New("notification helper is macOS only")
}

func LaunchedAsHelper() bool { return false }

func HelperMain(args []string) int { return 1 }
