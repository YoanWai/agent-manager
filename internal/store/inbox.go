package store

import (
	"database/sql"
	"errors"
	"time"
)

// InboxMessage is one agent-to-agent message waiting to be typed into a
// session's prompt. It rides its own table rather than PendingInputs
// because a launch prompt and a message need different delivery gates,
// and because a per-row claim survives a concurrent append from the MCP
// process, which the pending-input blob compare-and-set does not.
type InboxMessage struct {
	ID          int64
	SessionID   string
	SenderID    string
	SenderName  string
	Body        string
	Fingerprint string
	SentAt      time.Time
	ClaimedAt   time.Time
	DeliveredAt time.Time
	ReadAt      time.Time
}

// InboxLimits stop two agents talking each other into an infinite loop.
type InboxLimits struct {
	QueueCap     int
	RateCap      int
	RateWindow   time.Duration
	DedupeWindow time.Duration
}

var DefaultInboxLimits = InboxLimits{
	QueueCap:     20,
	RateCap:      5,
	RateWindow:   time.Minute,
	DedupeWindow: 10 * time.Minute,
}

// PollerHeartbeatKey is stamped by the manager on every poll. A session
// tool compares it against the poll interval to tell whether a manager is
// running to deliver what it queues.
const PollerHeartbeatKey = "poller_heartbeat"

var (
	ErrInboxFull        = errors.New("the recipient's queue is full; wait for it to read what is already queued")
	ErrInboxRateLimited = errors.New("too many messages to this session in the last minute")
	ErrInboxDuplicate   = errors.New("an identical message is already queued or was just sent")
)

// Enqueue appends one message. Every limit rides the INSERT itself, so a
// second process cannot slip past a check that ran as its own statement.
func (s *Store) Enqueue(msg InboxMessage, limits InboxLimits) (int64, error) {
	sentAt := encodeTime(msg.SentAt)
	rateFrom := encodeTime(msg.SentAt.Add(-limits.RateWindow))
	dedupeFrom := encodeTime(msg.SentAt.Add(-limits.DedupeWindow))
	res, err := s.db.Exec(`
INSERT INTO session_inbox (session_id, sender_id, sender_name, body, fingerprint, sent_at)
SELECT ?, ?, ?, ?, ?, ?
WHERE (SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND delivered_at = 0) < ?
  AND (SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND sender_id = ? AND sent_at >= ?) < ?
  AND NOT EXISTS (
    SELECT 1 FROM session_inbox
     WHERE session_id = ? AND sender_id = ? AND fingerprint = ? AND sent_at >= ?
  )`,
		msg.SessionID, msg.SenderID, msg.SenderName, msg.Body, msg.Fingerprint, sentAt,
		msg.SessionID, limits.QueueCap,
		msg.SessionID, msg.SenderID, rateFrom, limits.RateCap,
		msg.SessionID, msg.SenderID, msg.Fingerprint, dedupeFrom)
	if err != nil {
		return 0, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, s.rejectedEnqueue(msg, limits)
	}
	return res.LastInsertId()
}

// rejectedEnqueue names which guard turned the message away, so the
// sender learns whether to wait, slow down, or stop repeating itself.
func (s *Store) rejectedEnqueue(msg InboxMessage, limits InboxLimits) error {
	var queued, recent, duplicate int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND delivered_at = 0`,
		msg.SessionID).Scan(&queued); err != nil {
		return err
	}
	if queued >= limits.QueueCap {
		return ErrInboxFull
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND sender_id = ? AND sent_at >= ?`,
		msg.SessionID, msg.SenderID, encodeTime(msg.SentAt.Add(-limits.RateWindow))).Scan(&recent); err != nil {
		return err
	}
	if recent >= limits.RateCap {
		return ErrInboxRateLimited
	}
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND sender_id = ? AND fingerprint = ? AND sent_at >= ?`,
		msg.SessionID, msg.SenderID, msg.Fingerprint, encodeTime(msg.SentAt.Add(-limits.DedupeWindow))).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return ErrInboxDuplicate
	}
	return errors.New("message was not queued")
}

// HeadMessage returns the oldest message still waiting for delivery.
func (s *Store) HeadMessage(sessionID string) (InboxMessage, bool, error) {
	msg := InboxMessage{SessionID: sessionID}
	var sentAt, claimedAt int64
	err := s.db.QueryRow(`
SELECT id, sender_id, sender_name, body, fingerprint, sent_at, claimed_at
  FROM session_inbox
 WHERE session_id = ? AND delivered_at = 0
 ORDER BY id LIMIT 1`, sessionID).
		Scan(&msg.ID, &msg.SenderID, &msg.SenderName, &msg.Body, &msg.Fingerprint, &sentAt, &claimedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return InboxMessage{}, false, nil
	}
	if err != nil {
		return InboxMessage{}, false, err
	}
	msg.SentAt = decodeTime(sentAt)
	msg.ClaimedAt = decodeTime(claimedAt)
	return msg, true, nil
}

// ClaimMessage takes ownership of one message before it is typed. A
// concurrent append cannot invalidate this compare-and-set the way it can
// invalidate the pending-input one, because the guard is a single row.
func (s *Store) ClaimMessage(id int64, at time.Time) (bool, error) {
	res, err := s.db.Exec(
		`UPDATE session_inbox SET claimed_at = ? WHERE id = ? AND claimed_at = 0 AND delivered_at = 0`,
		encodeTime(at), id)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected == 1, err
}

func (s *Store) MarkDelivered(id int64, at time.Time) error {
	_, err := s.db.Exec(
		`UPDATE session_inbox SET delivered_at = ? WHERE id = ? AND delivered_at = 0`,
		encodeTime(at), id)
	return err
}

// MarkRead acks every message a session received from one sender. A reply
// is the ack: the recipient answering proves it read them.
func (s *Store) MarkRead(sessionID, senderID string, at time.Time) error {
	_, err := s.db.Exec(`
UPDATE session_inbox SET read_at = ?
 WHERE session_id = ? AND sender_id = ? AND delivered_at != 0 AND read_at = 0`,
		encodeTime(at), sessionID, senderID)
	return err
}

// Message reads back one message the given sender sent, so a sender can
// confirm delivery without reading the recipient's screen.
func (s *Store) Message(id int64, senderID string) (InboxMessage, error) {
	msg := InboxMessage{ID: id, SenderID: senderID}
	var sentAt, claimedAt, deliveredAt, readAt int64
	err := s.db.QueryRow(`
SELECT session_id, sender_name, body, fingerprint, sent_at, claimed_at, delivered_at, read_at
  FROM session_inbox WHERE id = ? AND sender_id = ?`, id, senderID).
		Scan(&msg.SessionID, &msg.SenderName, &msg.Body, &msg.Fingerprint, &sentAt, &claimedAt, &deliveredAt, &readAt)
	if err != nil {
		return InboxMessage{}, err
	}
	msg.SentAt = decodeTime(sentAt)
	msg.ClaimedAt = decodeTime(claimedAt)
	msg.DeliveredAt = decodeTime(deliveredAt)
	msg.ReadAt = decodeTime(readAt)
	return msg, nil
}

// QueuedCount is how many messages a session has waiting, which the sender
// reports as a queue position and the list view shows as a badge.
func (s *Store) QueuedCount(sessionID string) (int, error) {
	var queued int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM session_inbox WHERE session_id = ? AND delivered_at = 0`,
		sessionID).Scan(&queued)
	return queued, err
}

// QueuedCounts is every session's waiting count in one query, for the
// poller's per-tick refresh.
func (s *Store) QueuedCounts() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT session_id, COUNT(*) FROM session_inbox WHERE delivered_at = 0 GROUP BY session_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var id string
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		counts[id] = count
	}
	return counts, rows.Err()
}

// PruneInbox drops delivered messages past their retention window; the
// undelivered ones are the queue and are never swept.
func (s *Store) PruneInbox(before time.Time) error {
	_, err := s.db.Exec(
		`DELETE FROM session_inbox WHERE delivered_at != 0 AND sent_at < ?`,
		encodeTime(before))
	return err
}
