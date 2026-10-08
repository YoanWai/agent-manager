package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// Every operation passes its flags as --name=value and its operands after
// --, so neither a value nor an operand that starts with a dash, or is --
// itself, can be read as a flag by the remote CLI.

// Snapshot answers an outdated remote with what to update: one that does
// not know snapshot falls through to its interactive manager, which fails
// without a terminal after printing escape sequences instead of JSON.
func (c *Client) Snapshot(ctx context.Context, host string) (sessioncmd.Snapshot, error) {
	out, err := c.call(ctx, host, callTimeout, "snapshot", "--json")
	if err != nil {
		if !Unreachable(err) && len(bytes.TrimSpace(out)) > 0 && !json.Valid(out) {
			return sessioncmd.Snapshot{}, outdated(host)
		}
		return sessioncmd.Snapshot{}, err
	}
	var snapshot sessioncmd.Snapshot
	if err := json.Unmarshal(out, &snapshot); err != nil {
		return sessioncmd.Snapshot{}, outdated(host)
	}
	switch {
	case snapshot.Version < sessioncmd.SnapshotVersion:
		return sessioncmd.Snapshot{}, outdated(host)
	case snapshot.Version > sessioncmd.SnapshotVersion:
		return sessioncmd.Snapshot{}, &Error{Host: host, Err: errors.New("agent-manager there is newer than this one; update agent-manager on this machine")}
	}
	return snapshot, nil
}

func outdated(host string) error {
	return &Error{Host: host, Err: errors.New("agent-manager there is too old for SSH connections; update agent-manager on that host")}
}

// StartManager starts a headless manager on host when none is awake there.
func (c *Client) StartManager(ctx context.Context, host string) (bool, error) {
	var started struct {
		Started bool `json:"started"`
	}
	err := c.decode(ctx, host, callTimeout, &started, "serve", "--background")
	return started.Started, err
}

func (c *Client) Read(ctx context.Context, ref Ref, terminal bool) (string, error) {
	if err := validRef(ref); err != nil {
		return "", err
	}
	if terminal {
		var screen sessioncmd.TerminalScreen
		err := c.decode(ctx, ref.Host, callTimeout, &screen, "terminal", "read", "--json", "--", ref.ID)
		return screen.Output, err
	}
	var screen sessioncmd.SessionScreen
	err := c.decode(ctx, ref.Host, callTimeout, &screen, "read", "--json", "--", ref.ID)
	return screen.Output, err
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
	err := c.decode(ctx, host, createTimeout, &created, args...)
	return created, err
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
	err := c.decode(ctx, host, createTimeout, &created, args...)
	return created, err
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
	err := c.decode(ctx, ref.Host, callTimeout, &input, append(args, "--", ref.ID)...)
	return input, err
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
	err := c.decode(ctx, ref.Host, callTimeout, &session, append(args, "--", ref.ID)...)
	return session, err
}

func (c *Client) CreateGroup(ctx context.Context, host, path, directory string) (sessioncmd.Group, error) {
	args := []string{"create-group", "--json"}
	if directory != "" {
		args = append(args, "--directory="+directory)
	}
	var created sessioncmd.Group
	err := c.decode(ctx, host, callTimeout, &created, append(args, "--", path)...)
	return created, err
}

func (c *Client) DeleteGroup(ctx context.Context, host, path string) (sessioncmd.GroupRemoval, error) {
	var removal sessioncmd.GroupRemoval
	err := c.decode(ctx, host, callTimeout, &removal, "delete-group", "--json", "--", path)
	return removal, err
}

func validRef(ref Ref) error {
	if err := validID(ref.ID); err != nil {
		return &Error{Host: ref.Host, Err: err}
	}
	return nil
}
