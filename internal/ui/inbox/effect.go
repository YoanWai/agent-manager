package inbox

import (
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
)

// Services is what an inbox job reads the message log through.
type Services struct {
	Load func(sessionID string) (Log, error)
	Read func(messageID int64) (string, error)
}

// LoadRequest reads one session's queue and latest messages.
type LoadRequest struct {
	SessionID string
	Gen       uint64
}

func (LoadRequest) EffectName() string { return "inbox load" }

func (r LoadRequest) Run(services Services) (uieffects.Result, error) {
	return services.Load(r.SessionID)
}

// ReadRequest reads one message's whole text.
type ReadRequest struct {
	MessageID int64
	Gen       uint64
}

func (ReadRequest) EffectName() string { return "inbox message read" }

func (r ReadRequest) Run(services Services) (uieffects.Result, error) {
	return services.Read(r.MessageID)
}
