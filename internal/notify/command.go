package notify

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// commandTimeout bounds the user's notify command: a webhook that never
// answers costs one delivery, not a goroutine per transition forever.
var commandTimeout = 15 * time.Second

// commandOutputLimit is how much of a failing command's output the log
// keeps, enough for curl's error line without storing a response page.
const commandOutputLimit = 2 << 10

// commandLogLimit caps the failure log. Past it the log starts over, so a
// command that fails on every transition cannot fill the disk.
const commandLogLimit = 256 << 10

// CommandLog is the file failures of the notify command are appended to.
// The manager draws on the terminal, so stderr is not a place to put them,
// and a missed ping must never surface as an app error.
const CommandLog = "notify-command.log"

var runShell = runBoundedShell

func runBoundedShell(env []string, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), env...)
	output := &cappedBuffer{limit: commandOutputLimit}
	cmd.Stdout = output
	cmd.Stderr = output
	// A child that outlives sh keeps the output pipe open; this stops
	// waiting for it once sh is gone.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	return output.String(), err
}

// cappedBuffer keeps the start of a command's output and drops the rest,
// while reporting every write as taken so the pipes keep draining and a
// chatty command cannot grow the manager's memory.
type cappedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string {
	return b.buf.String()
}

// CommandEnv is the environment the notify command runs with, on top of
// the manager's own. Values carry agent-written text, so they travel as
// variables and never as part of the command line.
func CommandEnv(event Event) []string {
	detail, ok := describe(event.Kind)
	if !ok {
		return nil
	}
	title, body := content(event, detail)
	return []string{
		"AM_NOTIFY_KIND=" + kindName(event.Kind),
		"AM_SESSION_ID=" + event.ID,
		"AM_SESSION_NAME=" + sanitize(event.Session),
		"AM_TOOL=" + sanitize(event.Tool),
		"AM_CWD=" + event.Dir,
		"AM_BRANCH=" + sanitize(event.Branch),
		"AM_TITLE=" + title,
		"AM_BODY=" + body,
	}
}

func kindName(kind Kind) string {
	switch kind {
	case Waiting:
		return "waiting"
	case Finished:
		return "finished"
	case Errored:
		return "errored"
	}
	return ""
}

// RunCommand runs the user's notify command for one event through sh, in
// addition to the native banner. A failure, a non-zero exit or a timeout
// is appended to the log in configDir with what the command printed.
func RunCommand(command, configDir string, event Event) {
	env := CommandEnv(event)
	if strings.TrimSpace(command) == "" || env == nil {
		return
	}
	output, err := runShell(env, command)
	if err == nil {
		return
	}
	LogCommandFailure(configDir, event, fmt.Errorf("%w: %s", err, sanitize(output)))
}

// LogCommandFailure records a notify command that did not run as asked,
// whether it failed or could not be read from the store.
func LogCommandFailure(configDir string, event Event, failure error) {
	line := fmt.Sprintf("%s %s %s: %v\n", time.Now().Format(time.RFC3339), kindName(event.Kind), event.ID, failure)
	_ = appendLog(filepath.Join(configDir, CommandLog), line)
}

func appendLog(path, line string) error {
	if info, err := os.Stat(path); err == nil && info.Size() > commandLogLimit {
		if err := os.Truncate(path, 0); err != nil {
			return err
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = file.WriteString(line)
	return errors.Join(err, file.Close())
}
