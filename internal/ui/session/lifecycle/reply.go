package lifecycle

import (
	"context"
	"errors"
	"strings"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
)

// ReplyReader is what a reply copy reads a pane with: the tmux server, the
// status engine that knows where a tool's turn begins, and how many lines of
// history are worth keeping.
type ReplyReader struct {
	Driver  *tmux.Driver
	Engine  *status.Engine
	History int
}

type Reply struct {
	Text string
	// Unreadable is a tool that draws no region a reply can be read from,
	// which no amount of retrying will change.
	Unreadable bool
	// Unbounded is a pane that said nothing about where the turn began, so
	// the text is the whole screen rather than one answer.
	Unbounded bool
}

// Copy reads the newest answer a session gave, without entering it. The pane
// is captured whole, because the caller has no context of its own to bound the
// probe and the capture separately.
func (r ReplyReader) Copy(sess store.Session) (Reply, error) {
	ctx, cancel := context.WithTimeout(context.Background(), tmux.CommandTimeout)
	defer cancel()
	if !r.Driver.ExistsContext(ctx, sess.ID) {
		return Reply{}, errors.New(uisession.DeadHint)
	}
	pane, err := r.Driver.CapturePaneHistoryContext(ctx, sess.ID, r.History)
	if err != nil {
		return Reply{}, err
	}
	text, bounded, ok := r.Engine.FullTurnText(sess.Tool, r.Engine.Plain(sess.Tool, pane))
	if !ok {
		return Reply{Unreadable: true}, nil
	}
	return Reply{Text: strings.TrimSpace(text), Unbounded: !bounded}, nil
}
