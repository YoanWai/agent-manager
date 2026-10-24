//go:build windows

package startup

import "github.com/YoanWai/agent-manager/internal/pwsh"

// PaneShellWarning names what keeps sessions from starting when the pane
// shell is missing or predates PowerShell 7.3, empty when it is fine.
func PaneShellWarning() string {
	if _, err := pwsh.Path(); err != nil {
		return err.Error()
	}
	return ""
}
