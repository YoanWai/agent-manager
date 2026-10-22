package remote

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// Every operation passes its flags as --name=value and its operands after
// --, so neither a value nor an operand that starts with a dash, or is --
// itself, can be read as a flag by the remote CLI.

// Snapshot answers an outdated remote with what to update.
func (c *Client) Snapshot(ctx context.Context, host string) (sessioncmd.Snapshot, error) {
	var snapshot sessioncmd.Snapshot
	if err := c.decode(ctx, host, callTimeout, &snapshot, "snapshot", "--json"); err != nil {
		return sessioncmd.Snapshot{}, err
	}
	switch {
	case snapshot.Version < sessioncmd.SnapshotVersion:
		return sessioncmd.Snapshot{}, outdated(host)
	case snapshot.Version > sessioncmd.SnapshotVersion:
		return sessioncmd.Snapshot{}, newer(host)
	}
	return cleanSnapshot(snapshot), nil
}

// StartManager starts a headless manager on host when none is awake there.
func (c *Client) StartManager(ctx context.Context, host string) (bool, error) {
	var started struct {
		Started bool `json:"started"`
	}
	err := c.decode(ctx, host, callTimeout, &started, "serve", "--background")
	return started.Started, err
}

// ReadSession returns the row and the screen the host reports for ref.
func (c *Client) ReadSession(ctx context.Context, ref Ref) (sessioncmd.SessionScreen, error) {
	var screen sessioncmd.SessionScreen
	if err := validRef(ref); err != nil {
		return screen, err
	}
	if err := c.decode(ctx, ref.Host, callTimeout, &screen, "read", "--json", "--", ref.ID); err != nil {
		return sessioncmd.SessionScreen{}, err
	}
	session, err := cleanSession(screen.Session)
	if err != nil {
		return sessioncmd.SessionScreen{}, unreadable(ref.Host, "read", err)
	}
	return sessioncmd.SessionScreen{Session: session, Output: cleanScreen(screen.Output)}, nil
}

// ReadScreen is the screen of a session, or a terminal, in its colors, for
// a preview to paint. A host built before reads took --styled answers in
// plain text.
func (c *Client) ReadScreen(ctx context.Context, ref Ref, terminal bool) (string, error) {
	if err := validRef(ref); err != nil {
		return "", err
	}
	command := []string{"read"}
	if terminal {
		command = []string{"terminal", "read"}
	}
	var screen struct {
		Output string `json:"output"`
	}
	err := c.decode(ctx, ref.Host, callTimeout, &screen, append(command, "--styled", "--json", "--", ref.ID)...)
	if err == nil {
		return cleanStyledScreen(screen.Output), nil
	}
	if Unreachable(err) || ctx.Err() != nil {
		return "", err
	}
	if terminal {
		plain, err := c.ReadTerminal(ctx, ref)
		return plain.Output, err
	}
	plain, err := c.ReadSession(ctx, ref)
	return plain.Output, err
}

func (c *Client) ReadTerminal(ctx context.Context, ref Ref) (sessioncmd.TerminalScreen, error) {
	var screen sessioncmd.TerminalScreen
	if err := validRef(ref); err != nil {
		return screen, err
	}
	if err := c.decode(ctx, ref.Host, callTimeout, &screen, "terminal", "read", "--json", "--", ref.ID); err != nil {
		return sessioncmd.TerminalScreen{}, err
	}
	terminal, err := cleanTerminal(screen.Terminal)
	if err != nil {
		return sessioncmd.TerminalScreen{}, unreadable(ref.Host, "terminal read", err)
	}
	return sessioncmd.TerminalScreen{Terminal: terminal, Output: cleanScreen(screen.Output)}, nil
}

// Send queues text for the agent at ref, from a sender with no session on
// that host.
func (c *Client) Send(ctx context.Context, ref Ref, text, from string) (sessioncmd.SendResult, error) {
	var result sessioncmd.SendResult
	if err := validRef(ref); err != nil {
		return result, err
	}
	err := c.decode(ctx, ref.Host, callTimeout, &result, "send", "--from="+from, "--json", "--", ref.ID, text)
	return result, err
}

// Type submits prompt in the pane of the agent at ref now, as the user's
// quick bar does there.
func (c *Client) Type(ctx context.Context, ref Ref, prompt string) (sessioncmd.Session, error) {
	var typed sessioncmd.Session
	if err := validRef(ref); err != nil {
		return typed, err
	}
	if err := c.decode(ctx, ref.Host, callTimeout, &typed, "type", "--json", "--", ref.ID, prompt); err != nil {
		return typed, typeLost(err)
	}
	typed, err := cleanSession(typed)
	return typed, unreadable(ref.Host, "type", err)
}

// typeLost marks a type cut off after agent-manager started on the host,
// by the deadline or a dropped connection, as one that may have reached the
// agent: the paste may have run before the answer was lost.
func typeLost(err error) error {
	var failed *Error
	if !errors.As(err, &failed) || failed.mayHaveReached || !failed.ran {
		return err
	}
	if errors.Is(failed.Err, context.DeadlineExceeded) || failed.trouble == TroubleUnreachable {
		return &Error{Host: failed.Host, Err: fmt.Errorf("prompt may have reached the agent and was not sent again: %w", failed.Err), trouble: failed.trouble, ran: true, mayHaveReached: true}
	}
	return err
}

func (c *Client) Spawn(ctx context.Context, host string, opts sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	args := []string{"spawn", "--json"}
	for _, flag := range []struct{ name, value string }{
		{"name", opts.Name},
		{"prompt", opts.Prompt},
		{"tool", opts.Tool},
		{"model", opts.Model},
		{"effort", opts.Effort},
		{"profile", opts.Profile},
		{"directory", opts.Directory},
	} {
		if flag.value != "" {
			args = append(args, "--"+flag.name+"="+flag.value)
		}
	}
	if opts.Group != nil {
		args = append(args, "--group="+*opts.Group)
	}
	if opts.Worktree != nil {
		args = append(args, "--worktree="+strconv.FormatBool(*opts.Worktree))
	}
	var created sessioncmd.Session
	if err := c.decode(ctx, host, createTimeout, &created, args...); err != nil {
		return created, err
	}
	created, err := cleanSession(created)
	return created, unreadable(host, "spawn", err)
}

func (c *Client) CreateTerminal(ctx context.Context, host string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	args := []string{"terminal", "create", "--json"}
	if opts.Group != nil {
		args = append(args, "--group="+*opts.Group)
	}
	if opts.Directory != "" {
		args = append(args, "--directory="+opts.Directory)
	}
	if opts.Nest != nil {
		args = append(args, "--nest="+strconv.FormatBool(*opts.Nest))
	}
	var created sessioncmd.Terminal
	if err := c.decode(ctx, host, createTimeout, &created, args...); err != nil {
		return created, err
	}
	created, err := cleanTerminal(created)
	return created, unreadable(host, "terminal create", err)
}

// TerminalSend passes each key as its own --keys, since the CLI splits a
// value on commas.
func (c *Client) TerminalSend(ctx context.Context, ref Ref, command string, keys []string) (sessioncmd.TerminalInput, error) {
	var input sessioncmd.TerminalInput
	if err := validRef(ref); err != nil {
		return input, err
	}
	args := []string{"terminal", "send", "--json"}
	if command != "" {
		args = append(args, "--command="+command)
	}
	for _, key := range keys {
		args = append(args, "--keys="+key)
	}
	if err := c.decode(ctx, ref.Host, callTimeout, &input, append(args, "--", ref.ID)...); err != nil {
		return input, err
	}
	if err := validID(input.TerminalID); err != nil {
		return sessioncmd.TerminalInput{}, unreadable(ref.Host, "terminal send", err)
	}
	input.Sent = cleanText(input.Sent)
	return input, nil
}

// TerminalClose has no JSON form; the remote prints a sentence.
func (c *Client) TerminalClose(ctx context.Context, ref Ref) error {
	if err := validRef(ref); err != nil {
		return err
	}
	_, err := c.call(ctx, ref.Host, callTimeout, "terminal", "close", "--", ref.ID)
	return err
}

func (c *Client) Kill(ctx context.Context, ref Ref) (sessioncmd.Session, error) {
	return c.lifecycle(ctx, ref, "kill", "--json")
}

func (c *Client) Revive(ctx context.Context, ref Ref) (sessioncmd.Session, error) {
	return c.lifecycle(ctx, ref, "revive", "--json")
}

// Archive files the session out of the active list, or back with restore.
func (c *Client) Archive(ctx context.Context, ref Ref, restore bool) (sessioncmd.Session, error) {
	if restore {
		return c.lifecycle(ctx, ref, "archive", "--restore", "--json")
	}
	return c.lifecycle(ctx, ref, "archive", "--json")
}

func (c *Client) lifecycle(ctx context.Context, ref Ref, args ...string) (sessioncmd.Session, error) {
	var session sessioncmd.Session
	if err := validRef(ref); err != nil {
		return session, err
	}
	if err := c.decode(ctx, ref.Host, callTimeout, &session, append(args, "--", ref.ID)...); err != nil {
		return session, err
	}
	session, err := cleanSession(session)
	return session, unreadable(ref.Host, args[0], err)
}

func (c *Client) CreateGroup(ctx context.Context, host, path, directory string) (sessioncmd.Group, error) {
	args := []string{"create-group", "--json"}
	if directory != "" {
		args = append(args, "--directory="+directory)
	}
	var created sessioncmd.Group
	err := c.decode(ctx, host, callTimeout, &created, append(args, "--", path)...)
	return cleanGroup(created), err
}

func (c *Client) DeleteGroup(ctx context.Context, host, path string) (sessioncmd.GroupRemoval, error) {
	var removal sessioncmd.GroupRemoval
	err := c.decode(ctx, host, callTimeout, &removal, "delete-group", "--json", "--", path)
	return cleanRemoval(removal), err
}

func validRef(ref Ref) error {
	if err := validID(ref.ID); err != nil {
		return &Error{Host: ref.Host, Err: err}
	}
	return nil
}
