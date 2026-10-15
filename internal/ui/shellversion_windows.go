//go:build windows

package ui

import "github.com/YoanWai/agent-manager/internal/pwsh"

// paneShellWarning names what keeps sessions from starting when the pane
// shell is missing or predates PowerShell 7.3, empty when it is fine.
func paneShellWarning() string {
	if _, err := pwsh.Path(); err != nil {
		return err.Error()
	}
	return ""
}
