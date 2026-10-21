package remote

import (
	"context"
	"fmt"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

// Openers records which local session opened each terminal on a
// connection. Every SSH call reaches the host as one caller with no
// session, so only this side can tell one session's terminal from
// another's, or an agent's from the user's own T on the connection row.
type Openers interface {
	SetRemoteTerminalOpener(host, terminalID, sessionID string) error
	// RemoteTerminalOpener is empty for a terminal no local session opened.
	RemoteTerminalOpener(host, terminalID string) (string, error)
	DeleteRemoteTerminal(host, terminalID string) error
}

// Terminals holds the rule for terminals on connections: a session drives
// only the ones it opened, the way a session here drives only the
// terminals nested under it. The user drives any of them.
type Terminals struct {
	client  *Client
	openers Openers
}

func (c *Client) Terminals(openers Openers) Terminals {
	return Terminals{client: c, openers: openers}
}

// Open creates a terminal on host for session. One whose opener cannot be
// recorded is closed again, since nobody but the user could drive it.
func (t Terminals) Open(ctx context.Context, host, session string, opts sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	created, err := t.client.CreateTerminal(ctx, host, opts)
	if err != nil {
		return created, err
	}
	if err := t.openers.SetRemoteTerminalOpener(host, created.ID, session); err != nil {
		ref := Ref{Host: host, ID: created.ID}
		if closeErr := t.client.TerminalClose(ctx, ref); closeErr != nil {
			return sessioncmd.Terminal{}, fmt.Errorf("opened terminal %s, but recording this session as its opener failed (%w), and closing it failed too: %v", ref, err, closeErr)
		}
		return sessioncmd.Terminal{}, fmt.Errorf("recording this session as the opener of terminal %s failed, so it was closed again: %w", ref, err)
	}
	return created, nil
}

// Send types into a terminal session opened.
func (t Terminals) Send(ctx context.Context, ref Ref, session, command string, keys []string) (sessioncmd.TerminalInput, error) {
	if err := t.opened(ref, session); err != nil {
		return sessioncmd.TerminalInput{}, err
	}
	return t.client.TerminalSend(ctx, ref, command, keys)
}

// Close closes a terminal session opened.
func (t Terminals) Close(ctx context.Context, ref Ref, session string) error {
	if err := t.opened(ref, session); err != nil {
		return err
	}
	return t.CloseForUser(ctx, ref)
}

// CloseForUser closes any terminal on the connection, as the user may from
// its row, and forgets who opened it.
func (t Terminals) CloseForUser(ctx context.Context, ref Ref) error {
	if err := t.client.TerminalClose(ctx, ref); err != nil {
		return err
	}
	if err := t.openers.DeleteRemoteTerminal(ref.Host, ref.ID); err != nil {
		return fmt.Errorf("closed terminal %s, but forgetting which session opened it failed: %w", ref, err)
	}
	return nil
}

func (t Terminals) opened(ref Ref, session string) error {
	opener, err := t.openers.RemoteTerminalOpener(ref.Host, ref.ID)
	if err != nil {
		return err
	}
	if session == "" || opener != session {
		return fmt.Errorf("terminal %s was not opened by this session; a session drives only the terminals it opened on %s with create_terminal", ref, ref.Host)
	}
	return nil
}
