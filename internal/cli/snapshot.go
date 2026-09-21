package cli

import (
	"io"

	"github.com/YoanWai/agent-manager/internal/federation"
)

const usageSnapshot = "snapshot [--json]"

func snapshotSection() section {
	return section{title: "Federation", commands: []command{{
		name: "snapshot", usage: usageSnapshot,
		about: "return one consistent host inventory for another Agent Manager",
		run:   configCommand(runSnapshot),
	}}}
}

func runSnapshot(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageSnapshot)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	if !*asJSON {
		return usageError(usageSnapshot)
	}
	sessions := newSessions(configDir)
	terminals := newTerminals(configDir)
	listed, err := sessions.List(sessionID)
	if err != nil {
		return err
	}
	shells, err := terminals.List(sessionID)
	if err != nil {
		return err
	}
	groups, err := sessions.Groups(sessionID)
	if err != nil {
		return err
	}
	return writeJSON(out, federation.SnapshotEnvelope{Version: 1, Sessions: listed, Terminals: shells, Groups: groups})
}
