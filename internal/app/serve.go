package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// ServeLog is the file in the profile directory a detached manager's
// output is appended to.
const ServeLog = "serve.log"

// ServeLock is the file in the profile directory a running serve holds an
// exclusive lock on for its lifetime.
const ServeLock = "serve.lock"

// BackgroundStart is what `serve --background` prints.
type BackgroundStart struct {
	Started bool `json:"started"`
	PID     int  `json:"pid,omitempty"`
}

// Starter starts `agent-manager <args>` detached from the caller, appending
// its output to logPath, and returns its pid without waiting for it.
type Starter func(args []string, logPath string) (int, error)

// Serve runs the poller with no TUI until ctx ends. Of what the TUI does
// with each pass, only the archive or kill a session asked for once its
// turn ended needs no screen, so that is carried out here. A failed pass is
// reported and the next one runs regardless.
func (l *Local) Serve(ctx context.Context, errs io.Writer) {
	for result := range l.Execution.Run(ctx) {
		if result.Err != nil {
			fmt.Fprintln(errs, "agent-manager serve:", result.Err)
			continue
		}
		for _, id := range result.Snapshot.TurnsEnded {
			if _, err := l.Lifecycle.EndAfterTurn(id); err != nil {
				fmt.Fprintf(errs, "agent-manager serve: end session %s after its turn: %v\n", id, err)
			}
		}
	}
}

// LockServe takes the profile's serve lock, which a serve holds until it
// calls release. acquired is false while another serve holds it.
func LockServe(profileDir string) (release func(), acquired bool, err error) {
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		return nil, false, err
	}
	return tryServeLock(filepath.Join(profileDir, ServeLock))
}

// ServeBackground starts a detached `serve` unless one holds the profile's
// lock, which it does before its first poll, or a manager is already
// polling this profile, which already delivers what is queued here.
func ServeBackground(profileDir string, now time.Time, start Starter) (BackgroundStart, error) {
	release, acquired, err := LockServe(profileDir)
	if err != nil || !acquired {
		return BackgroundStart{}, err
	}
	release()
	st, err := store.Open(filepath.Join(profileDir, "state.db"))
	if err != nil {
		return BackgroundStart{}, err
	}
	awake, err := st.ManagerAwake(now)
	if closeErr := st.Close(); err == nil {
		err = closeErr
	}
	if err != nil || awake {
		return BackgroundStart{}, err
	}
	pid, err := start([]string{"serve"}, filepath.Join(profileDir, ServeLog))
	if err != nil {
		return BackgroundStart{}, err
	}
	return BackgroundStart{Started: true, PID: pid}, nil
}
