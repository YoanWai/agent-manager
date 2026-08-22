package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/YoanWai/agent-manager/internal/update"
)

const usageUpdate = "update [--json]"

// The seams let tests steer the update path without hitting GitHub or
// swapping the running binary.
var (
	updateExecutable = os.Executable
	updateDetect     = update.DetectManager
	updateRefresh    = update.Refresh
	updateApply      = update.Apply
)

func updateSection(version string) section {
	return section{
		title: "Updates",
		commands: []command{
			{
				name:  "update",
				usage: usageUpdate,
				about: "bring agent-manager to its newest release, through its package manager or in place",
				run:   configCommand(runUpdate(version)),
			},
		},
	}
}

// updateReport is the --json record; exactly one field names what happened.
type updateReport struct {
	Version  string `json:"version,omitempty"`
	Delegate string `json:"delegate,omitempty"`
	UpToDate bool   `json:"up_to_date,omitempty"`
}

func runUpdate(version string) func(out io.Writer, args []string, sessionID, configDir string) error {
	return func(out io.Writer, args []string, sessionID, configDir string) error {
		set := newFlagSet(usageUpdate)
		asJSON := jsonFlag(set)
		if _, err := parseCommand(out, set, args, 0, 0); err != nil {
			return err
		}

		execPath, err := updateExecutable()
		if err != nil {
			return err
		}

		manager := updateDetect(execPath)
		if manager.Advice != "" {
			return errors.New(manager.Advice)
		}
		if manager.Delegated() {
			command := exec.Command(manager.Command[0], manager.Command[1:]...)
			command.Stdin = os.Stdin
			command.Stderr = os.Stderr
			if *asJSON {
				// A machine parses stdout as one record, so the manager's
				// own progress stays out of it.
				command.Stdout = os.Stderr
			} else {
				command.Stdout = out
			}
			if err := command.Run(); err != nil {
				return fmt.Errorf("%s: %w", manager.String(), err)
			}
			return emit(out, *asJSON, updateReport{Delegate: manager.String()},
				fmt.Sprintf("updated with %s", manager.String()))
		}

		if !update.VersionWithin(version, "0.0.0", "") {
			// A dev build carries no release tag, so there is nothing to
			// compare against or download.
			return errors.New("update: this build is not a release; reinstall to upgrade")
		}
		result, err := updateRefresh(context.Background(), configDir, version)
		if err != nil {
			return fmt.Errorf("update: could not reach GitHub: %w", err)
		}
		if result.Latest == "" {
			return emit(out, *asJSON, updateReport{UpToDate: true}, "already up to date")
		}
		// The download can take a while, so say what is coming; --json keeps
		// stdout to the single record.
		if !*asJSON {
			if _, err := fmt.Fprintf(out, "updating to %s...\n", result.Latest); err != nil {
				return err
			}
		}
		if err := updateApply(context.Background(), result.Latest, execPath); err != nil {
			return fmt.Errorf("update: %w", err)
		}
		return emit(out, *asJSON, updateReport{Version: result.Latest},
			fmt.Sprintf("updated to %s", result.Latest))
	}
}
