package install

import (
	"errors"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
)

// FixFor classifies a refused launch: a missing prerequisite or a CLI older
// than the manager supports becomes the card that names the command
// unblocking it, and anything else is not this package's to show.
func FixFor(err error) (Fix, bool) {
	var hermesMCP mcpreg.HermesMCPUnavailableError
	if errors.As(err, &hermesMCP) {
		return Fix{
			Text:    HermesText(hermesMCP.PipCommand),
			Command: hermesMCP.PipCommand,
			Script:  hermesMCP.PipCommand,
			Binary:  "hermes",
		}, true
	}
	var missing config.MissingToolError
	if errors.As(err, &missing) {
		tool := Tool{Binary: missing.Binary, WindowsPath: missing.WindowsPath}
		return Fix{
			Text:    MissingText(tool, deps.Hint(missing.Binary)),
			Command: deps.Shown(missing.Binary),
			Script:  deps.Command(missing.Binary),
			Binary:  missing.Binary,
		}, true
	}
	var outdated config.OutdatedToolError
	if errors.As(err, &outdated) {
		return OutdatedFix(outdated, OutdatedCommand(outdated)), true
	}
	return Fix{}, false
}

// OutdatedFix is the card a CLI older than the manager supports opens, and
// the same card an answered catalog opens for a version the manager cannot
// run.
func OutdatedFix(outdated config.OutdatedToolError, command string) Fix {
	return Fix{
		Text:    OutdatedText(outdatedTool(outdated), command),
		Command: command,
		Script:  command,
		Binary:  outdated.Binary,
		Update:  true,
	}
}

// OutdatedCommand is the installer line a too-old CLI updates with, empty
// when the manager ships none and the user updates the way they installed it.
func OutdatedCommand(outdated config.OutdatedToolError) string {
	if outdated.ManagedExternally {
		return ""
	}
	return deps.Command(outdated.Binary)
}

func outdatedTool(outdated config.OutdatedToolError) Tool {
	return Tool{
		Binary:    outdated.Binary,
		Installed: outdated.Installed,
		Minimum:   outdated.Minimum,
	}
}
