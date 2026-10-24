package link

import (
	"context"

	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	tea "github.com/charmbracelet/bubbletea"
)

// Services is the work a call or a save needs done off the update path: the
// table the connection list lives in, the client that reaches the hosts, and
// the openers that tell whose terminal is whose.
type Services struct {
	Store   *store.Store
	Client  *remote.Client
	Openers remote.Openers
}

// EffectName names the save for a quit notice.
func (SaveRequest) EffectName() string { return "SSH connection save" }

func (r SaveRequest) EffectCopy() uieffects.Request { return r }

// Run writes the list and reads it back in the same job.
func (r SaveRequest) Run(services Services) (uieffects.Result, error) {
	connections, err := Save(services.Store, r)
	return SaveResult{Connections: connections}, err
}

// SaveResult is the list as the store holds it after the write.
type SaveResult struct {
	Connections []store.Connection
}

func (SaveResult) EffectResult() {}

// EffectName names the call for a quit notice.
func (Call) EffectName() string { return "call to an SSH connection" }

func (c Call) EffectCopy() uieffects.Request { return c }

// Run makes the call and returns what it left.
func (c Call) Run(services Services) (uieffects.Result, error) {
	return Run(context.Background(), services.Client, services.Openers, c)
}

// EffectResult marks what a call left as a job result, so the manager applies
// it through its own completion.
func (Result) EffectResult() {}

// SaveMsg is what a key on the connection card asked of the manager: save a
// connection, or only clear the status line the card is leaving behind. The
// card returns it from a command; the manager answers it on Update.
type SaveMsg struct {
	Save *SaveRequest
}

// Route answers the card's own message, reporting whether msg was one. Every
// answer clears the status line the card leaves; a save also hands on the
// request to queue, nil for a cancel.
func Route(msg tea.Msg) (save *SaveRequest, ok bool) {
	card, ok := msg.(SaveMsg)
	if !ok {
		return nil, false
	}
	return card.Save, true
}
