package sessioncmd

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

type SendResult struct {
	MessageID     int64 `json:"message_id" jsonschema:"pass to message_status to see whether it arrived"`
	QueuePosition int   `json:"queue_position" jsonschema:"this message's place in the recipient's queue; 1 means it is next"`
	ManagerAwake  bool  `json:"manager_awake" jsonschema:"whether Agent Manager is running to deliver it; a message queued while it is closed waits until it opens again"`
}

// maxMessageBytes bounds one message. An instruction to another agent is
// prose; the queue and rate caps count messages, and this is what keeps one
// of them from being a file paste that fills the recipient's prompt.
const maxMessageBytes = 8000

// Send queues a message for another agent. It is deliberately not typed
// into the pane here: several tools keep their input line drawn under an
// approval dialog, so a message sent the moment it is written would answer
// that dialog. The manager's poller types it in once the target is at rest.
func (s *Sessions) Send(sessionID, targetID, message string) (SendResult, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return SendResult{}, errors.New("message is empty")
	}
	if len(message) > maxMessageBytes {
		return SendResult{}, fmt.Errorf("message is %d bytes, over the %d byte limit; shorten it to the instruction and point the agent at a file or a task for the detail", len(message), maxMessageBytes)
	}
	runtime, err := s.open()
	if err != nil {
		return SendResult{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return SendResult{}, err
	}
	target, err := runtime.agent(targetID)
	if err != nil {
		return SendResult{}, err
	}
	if target.ID == caller.ID {
		return SendResult{}, errors.New("a session cannot message itself")
	}
	if err := runtime.deliverable(target); err != nil {
		return SendResult{}, err
	}
	now := time.Now()
	id, err := runtime.store.Enqueue(store.InboxMessage{
		SessionID:   target.ID,
		SenderID:    caller.ID,
		SenderName:  caller.Name,
		Body:        message,
		Fingerprint: fingerprint(message),
		SentAt:      now,
	}, store.DefaultInboxLimits)
	if err != nil {
		return SendResult{}, err
	}
	// Answering is the acknowledgement: whatever this session was sent by
	// the agent it is now writing to has plainly been read.
	if err := runtime.store.MarkRead(caller.ID, target.ID, now); err != nil {
		return SendResult{}, err
	}
	queued, err := runtime.store.QueuedCount(target.ID)
	if err != nil {
		return SendResult{}, err
	}
	awake, err := runtime.managerAwake(now)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{MessageID: id, QueuePosition: queued, ManagerAwake: awake}, nil
}

// MessageStatus reports what happened to a message this session sent.
func (s *Sessions) MessageStatus(sessionID string, messageID int64) (MessageState, error) {
	runtime, err := s.open()
	if err != nil {
		return MessageState{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return MessageState{}, err
	}
	msg, err := runtime.store.Message(messageID, caller.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageState{}, fmt.Errorf("message %d was not sent by this session, or has aged out of the log", messageID)
	}
	if err != nil {
		return MessageState{}, err
	}
	state := MessageState{
		MessageID: msg.ID,
		SessionID: msg.SessionID,
		Body:      msg.Body,
	}
	switch {
	// A drop stamps the delivery column as well, so that a message nothing
	// will ever type leaves the queue, and is read first for that reason.
	case !msg.DroppedAt.IsZero():
		state.State = "dropped"
		state.Reason = "Agent Manager could not type it into the pane, and never retries a message; send it again"
	case !msg.ReadAt.IsZero():
		state.State = "answered"
		state.DeliveredAt = msg.DeliveredAt.Format(time.RFC3339)
	case !msg.DeliveredAt.IsZero():
		state.State = "delivered"
		state.DeliveredAt = msg.DeliveredAt.Format(time.RFC3339)
	default:
		state.State = "queued"
		held, err := runtime.heldReason(msg.SessionID)
		if err != nil {
			return MessageState{}, err
		}
		if held != "" {
			state.State = "held"
			state.Reason = held
		}
	}
	return state, nil
}

// heldReason names why a queued message is not moving when the recipient is
// what stops it: a session the manager no longer visits, or a screen the
// delivery gate refuses. The gate is the poller's own, run here against the
// recipient's current pane, so a sender is never promised a hold the manager
// does not keep. A recipient merely mid-turn is not held: that clears itself.
func (r *runtime) heldReason(sessionID string) (string, error) {
	target, err := r.store.Get(sessionID)
	if err != nil {
		return "", err
	}
	if target.Archived {
		return fmt.Sprintf("session %s is archived, so Agent Manager no longer polls it and nothing will type this in; restore it with %s", sessionID, r.words.Restore), nil
	}
	if !r.driver.Exists(target.ID) {
		return fmt.Sprintf("session %s is not running, so nothing will type this in; revive it with %s", sessionID, r.words.Revive), nil
	}
	if r.cfg.Tools[target.Tool].ActivityCutoff == "" {
		return fmt.Sprintf("Agent Manager cannot tell when %s is ready, so nothing will type this in: %s", sessionID, unreadableTool(r.cfg, target.Tool)), nil
	}
	// The poller types into a resting session, and an errored one is not
	// resting: it is showing whatever stopped it, often a limit its agent
	// cannot clear on its own.
	if target.Status == status.Errored {
		return fmt.Sprintf("session %s is errored, so nothing will type this in until it recovers; read its screen with %s", sessionID, r.words.Read), nil
	}
	pane, err := r.driver.CapturePane(target.ID)
	if err != nil {
		return "", err
	}
	engine, err := status.NewEngine(r.cfg)
	if err != nil {
		return "", err
	}
	if engine.TypingHold(target.Tool, ansi.Strip(pane)) != status.Waiting {
		return "", nil
	}
	return fmt.Sprintf("session %s is sitting on a dialog, and nothing is typed into a session while one is on its screen; read that screen and answer it", sessionID), nil
}

type MessageState struct {
	MessageID   int64  `json:"message_id"`
	SessionID   string `json:"session_id" jsonschema:"session the message was addressed to"`
	Body        string `json:"body"`
	State       string `json:"state" jsonschema:"queued (waiting for the agent to be at rest), held (nothing will type it in as things stand: the recipient is sitting on a dialog, archived or not running, and reason says which), delivered (typed into its prompt), dropped (it never reached the prompt and is not retried), or answered (it has since messaged back)"`
	DeliveredAt string `json:"delivered_at,omitempty" jsonschema:"RFC3339 time the message reached the prompt"`
	Reason      string `json:"reason,omitempty" jsonschema:"why the message is in that state, and what to do about it"`
}

// managerAwake reports whether a manager has polled recently enough to
// still be delivering. Queued messages only move while it runs.
func (r *runtime) managerAwake(now time.Time) (bool, error) {
	return r.store.ManagerAwake(now, r.cfg.PollInterval.Duration)
}

// fingerprint collapses whitespace before hashing so a retry that only
// re-wraps its text is still recognised as the same message.
func fingerprint(message string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(message), " ")))
	return hex.EncodeToString(sum[:])
}
