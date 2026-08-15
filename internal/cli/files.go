package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageReserve      = "reserve <path>... [--mode exclusive|shared] [--note <text>] [--ttl <duration>] [--json]"
	usageReleaseFiles = "release-files [<path>...] [--json]"
	usageReservations = "reservations [--json]"
)

type fileCommands interface {
	Reserve(sessionID string, patterns []string, mode, note string, ttl time.Duration) (sessioncmd.ReserveResult, error)
	ReleaseFiles(sessionID string, patterns []string) (int, error)
	Reservations(sessionID string) ([]sessioncmd.Reservation, error)
}

func newFiles(configDir string) fileCommands {
	return sessioncmd.NewSessions(configDir, sessioncmd.CLIVocabulary())
}

func fileSection() section {
	return section{
		title: "File reservations",
		commands: []command{
			{name: "reserve", usage: usageReserve, about: "declare the files you are about to edit, so another agent in the same checkout finds out before both of you change them", run: bind(newFiles, runReserve)},
			{name: "release-files", usage: usageReleaseFiles, about: "give the leases back once the edits are made; name no path to release everything you hold", run: bind(newFiles, runReleaseFiles)},
			{name: "reservations", usage: usageReservations, about: "see which files the other sessions are working on right now", run: bind(newFiles, runReservations)},
		},
	}
}

type releasedCount struct {
	Released int `json:"released"`
}

func runReserve(out io.Writer, files fileCommands, args []string, sessionID string) error {
	set := newFlagSet(usageReserve)
	mode := set.String("mode", "", "exclusive (default) means nobody else should edit these paths; shared means others may too")
	note := set.String("note", "", "what you are doing there, so a conflicting agent knows what it is up against")
	ttl := set.Duration("ttl", 0, "how long the lease lasts before it lapses, default "+sessioncmd.DefaultReservationTTL.String()+", maximum "+sessioncmd.MaxReservationTTL.String())
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 1, anyNumber)
	if err != nil {
		return err
	}
	if err := checkPaths(usageReserve, operands); err != nil {
		return err
	}
	result, err := files.Reserve(sessionID, operands, *mode, *note, *ttl)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, result, sessioncmd.FormatReserveResult(result))
}

func runReleaseFiles(out io.Writer, files fileCommands, args []string, sessionID string) error {
	set := newFlagSet(usageReleaseFiles)
	asJSON := jsonFlag(set)
	operands, err := parseCommand(out, set, args, 0, anyNumber)
	if err != nil {
		return err
	}
	if err := checkPaths(usageReleaseFiles, operands); err != nil {
		return err
	}
	released, err := files.ReleaseFiles(sessionID, operands)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, releasedCount{Released: released}, sessioncmd.FormatReleased(released))
}

func runReservations(out io.Writer, files fileCommands, args []string, sessionID string) error {
	set := newFlagSet(usageReservations)
	asJSON := jsonFlag(set)
	if _, err := parseCommand(out, set, args, 0, 0); err != nil {
		return err
	}
	listed, err := files.Reservations(sessionID)
	if err != nil {
		return err
	}
	return emit(out, *asJSON, listed, sessioncmd.FormatReservations(listed))
}

// A lease is taken on whatever string it is handed, so a mistyped flag the
// set does not know would otherwise be recorded as a pattern that can never
// conflict.
func checkPaths(usage string, paths []string) error {
	for _, path := range paths {
		if strings.HasPrefix(path, "-") {
			return fmt.Errorf("%q starts with a dash, so it is refused rather than leased as a pattern nothing can ever conflict with; usage: agent-manager %s", path, usage)
		}
	}
	return nil
}
