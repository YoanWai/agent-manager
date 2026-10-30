package ui

import "os/exec"

// startReaped starts cmd without waiting for it and reaps it in the
// background. The manager runs for days at a time; a child started and never
// waited on stays behind as a zombie, one per launch, until the manager exits.
func startReaped(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
