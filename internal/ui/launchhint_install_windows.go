//go:build windows

package ui

import (
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/pwsh"
)

// installScript shows the command, runs it, and records how it ended. The
// status write sits in finally, so an installer that exits or a Ctrl+C
// still records a status, 130 when the command never finished. A command
// that throws (one not found among them, which sh reports as 127) lands in
// catch instead, which Ctrl+C never reaches. The whole thing is a file so
// the pane's shell is typed one short line: an installer's own quoting then
// reaches PowerShell unchanged whatever shell the pane runs.
func installScript(command, statusFile string) string {
	return "$code = 130\n" +
		"try {\n" +
		"  Write-Host " + pwsh.Quote("$ "+command) + "\n" +
		"  $global:LASTEXITCODE = 0\n" +
		"  " + command + "\n" +
		"  $ok = $?\n" +
		"  $code = if ($LASTEXITCODE) { $LASTEXITCODE } elseif ($ok) { 0 } else { 1 }\n" +
		"} catch {\n" +
		"  $Host.UI.WriteErrorLine([string]$_)\n" +
		"  $code = if ($_.Exception -is [System.Management.Automation.CommandNotFoundException]) { 127 } else { 1 }\n" +
		"} finally {\n" +
		"  [IO.File]::WriteAllText(" + pwsh.Quote(statusFile) + ", [string]$code)\n" +
		"}\n"
}

// installLine is what the pane's shell is typed to run the install script.
// A bare double-quoted path reads the same in pwsh, powershell, cmd and
// bash, whichever the pane runs; the shell named is the one Path found.
func installLine(script string) (string, error) {
	shell, err := pwsh.Path()
	if err != nil {
		return "", err
	}
	return filepath.Base(shell) + ` -NoProfile -ExecutionPolicy Bypass -File "` + script + `"`, nil
}
