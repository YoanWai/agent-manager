package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/update"
)

const usageUpdate = "update [--connection=<name>] [--json]"

// The seams let tests steer the update path without hitting GitHub or
// swapping the running binary.
var (
	updateExecutable = os.Executable
	updateDetect     = update.DetectManager
	updateRefresh    = update.Refresh
	updateApply      = update.Apply
	updateRemote     = func(configDir string) *remote.Client { return remote.New(configDir) }
)

func updateSection(version string) section {
	return section{
		title: "Updates",
		commands: []command{
			{
				name:  "update",
				usage: usageUpdate,
				about: "bring agent-manager to its newest release, through its package manager or in place; with --connection, install this build's release in ~/.local/bin on that SSH connection's host",
				run: func(args []string, _ func() string, configDir string) error {
					return runUpdate(version)(os.Stdout, args, configDir)
				},
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

func runUpdate(version string) func(out io.Writer, args []string, configDir string) error {
	return func(out io.Writer, args []string, configDir string) error {
		set := newFlagSet(usageUpdate)
		asJSON := jsonFlag(set)
		connection := set.String("connection", "", "the SSH connection whose host gets this build's release")
		if _, err := parseCommand(out, set, args, 0, 0); err != nil {
			return err
		}
		if *connection != "" {
			return installOnConnection(out, version, configDir, *connection, *asJSON)
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

// connectionReport is the --json record of an install on a connection's
// host.
type connectionReport struct {
	Connection string `json:"connection"`
	remote.Installed
	Answers bool   `json:"answers"`
	Advice  string `json:"advice,omitempty"`
}

// installOnConnection puts this build's release on the host and asks it
// for a snapshot again. A host this one cannot reach, or one newer than
// this build, is left alone.
func installOnConnection(out io.Writer, version, configDir, name string, asJSON bool) error {
	tag, err := remote.InstallTag(version)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	st, err := store.Open(filepath.Join(configDir, "state.db"))
	if err != nil {
		return err
	}
	stored, err := st.Connections()
	st.Close()
	if err != nil {
		return err
	}
	connections := make([]remote.Connection, 0, len(stored))
	found := false
	for _, conn := range stored {
		connections = append(connections, remote.Connection{Name: conn.Name, Destination: conn.Destination})
		found = found || conn.Name == name
	}
	if !found {
		return fmt.Errorf("update: no SSH connection is named %q", name)
	}
	client := updateRemote(configDir)
	client.SetConnections(connections)
	ctx := context.Background()
	if _, err := client.Snapshot(ctx, name); err != nil && !remote.Installable(err) {
		return fmt.Errorf("update: %w", err)
	}
	if !asJSON {
		if _, err := fmt.Fprintf(out, "installing agent-manager %s on %s...\n", tag, name); err != nil {
			return err
		}
	}
	installed, err := client.Install(ctx, name, version)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	report := connectionReport{Connection: name, Installed: installed, Advice: installed.PathAdvice()}
	_, err = client.Snapshot(ctx, name)
	report.Answers = err == nil
	human := fmt.Sprintf("installed agent-manager %s for %s/%s at %s on %s", installed.Version, installed.OS, installed.Arch, installed.Path, name)
	if report.Advice != "" {
		human += "; " + report.Advice
	}
	if emitErr := emit(out, asJSON, report, human); emitErr != nil {
		return emitErr
	}
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	return nil
}
