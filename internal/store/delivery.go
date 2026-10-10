package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

type DeliveryOutcome string

const (
	DeliveryInFlight  DeliveryOutcome = "in_flight"
	DeliveryConfirmed DeliveryOutcome = "confirmed"
	DeliveryRefused   DeliveryOutcome = "refused"
	DeliveryUncertain DeliveryOutcome = "uncertain"
)

var (
	ErrDeliveryGuardRequired   = errors.New("automatic delivery write requires the profile delivery guard")
	ErrDeliveryReceiptRejected = errors.New("delivery receipt does not match the admitted attempt")
)

type DeliveryClaim struct {
	AttemptToken string
	ClaimedAt    time.Time
}

// DeliveryGuard is a callback-scoped capability. It is backed by a
// nonblocking process lock and an OS advisory lock beside the profile DB.
// Only automatic inbox and pending-input delivery participates in this lock.
type DeliveryGuard struct {
	store  *Store
	active bool
}

var deliveryProcessLocks sync.Map // absolute DB path -> *sync.Mutex

// WithDeliveryGuard runs fn while this process owns the profile's automatic
// delivery lane. A busy lane returns acquired=false immediately.
func (s *Store) WithDeliveryGuard(ctx context.Context, fn func(*DeliveryGuard) error) (acquired bool, err error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	value, _ := deliveryProcessLocks.LoadOrStore(s.path, &sync.Mutex{})
	processLock := value.(*sync.Mutex)
	if !processLock.TryLock() {
		return false, nil
	}
	defer processLock.Unlock()

	unlockFile, acquired, err := tryDeliveryFileLock(s.path + ".delivery.lock")
	if err != nil || !acquired {
		return acquired, err
	}
	defer unlockFile()
	if err := ctx.Err(); err != nil {
		return false, err
	}

	guard := &DeliveryGuard{store: s, active: true}
	defer func() { guard.active = false }()
	return true, fn(guard)
}

func (g *DeliveryGuard) requireActive() error {
	if g == nil || !g.active || g.store == nil {
		return ErrDeliveryGuardRequired
	}
	return nil
}

func newDeliveryToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("create delivery attempt token: %w", err)
	}
	return hex.EncodeToString(raw[:]), nil
}

func terminalDeliveryOutcome(outcome DeliveryOutcome) bool {
	return outcome == DeliveryConfirmed || outcome == DeliveryRefused || outcome == DeliveryUncertain
}

func (g *DeliveryGuard) HeadMessage(sessionID string) (InboxMessage, bool, error) {
	if err := g.requireActive(); err != nil {
		return InboxMessage{}, false, err
	}
	return g.store.HeadMessage(sessionID)
}

func (g *DeliveryGuard) ClaimMessage(id int64, at time.Time) (DeliveryClaim, bool, error) {
	if err := g.requireActive(); err != nil {
		return DeliveryClaim{}, false, err
	}
	token, err := newDeliveryToken()
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	res, err := g.store.db.Exec(`
UPDATE session_inbox
   SET claimed_at = ?, attempt_token = ?, receipt_token = '', delivery_outcome = ?
 WHERE id = ? AND claimed_at = 0 AND delivered_at = 0`,
		encodeTime(at), token, DeliveryInFlight, id)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		return DeliveryClaim{}, false, err
	}
	return DeliveryClaim{AttemptToken: token, ClaimedAt: at}, true, nil
}

func (g *DeliveryGuard) FinishMessage(id int64, claim DeliveryClaim, outcome DeliveryOutcome, at time.Time) error {
	if err := g.requireActive(); err != nil {
		return err
	}
	if claim.AttemptToken == "" || !terminalDeliveryOutcome(outcome) {
		return ErrDeliveryReceiptRejected
	}
	stamp := encodeTime(at)
	droppedAt := int64(0)
	if outcome == DeliveryRefused {
		droppedAt = stamp
	}
	res, err := g.store.db.Exec(`
UPDATE session_inbox
   SET delivered_at = ?, dropped_at = ?, receipt_token = ?, delivery_outcome = ?
 WHERE id = ? AND delivered_at = 0 AND attempt_token = ? AND delivery_outcome = ?`,
		stamp, droppedAt, claim.AttemptToken, outcome, id, claim.AttemptToken, DeliveryInFlight)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrDeliveryReceiptRejected
	}
	return nil
}

func (g *DeliveryGuard) RecoverMessage(id int64, at time.Time) error {
	if err := g.requireActive(); err != nil {
		return err
	}
	var token string
	var outcome DeliveryOutcome
	var deliveredAt int64
	if err := g.store.db.QueryRow(
		`SELECT attempt_token, delivery_outcome, delivered_at FROM session_inbox WHERE id = ?`, id,
	).Scan(&token, &outcome, &deliveredAt); err != nil {
		return err
	}
	if deliveredAt != 0 || token == "" || outcome != DeliveryInFlight {
		return ErrDeliveryReceiptRejected
	}
	return g.FinishMessage(id, DeliveryClaim{AttemptToken: token}, DeliveryUncertain, at)
}

func (g *DeliveryGuard) ClaimPendingInput(id, expected string, at time.Time) (DeliveryClaim, bool, error) {
	if err := g.requireActive(); err != nil {
		return DeliveryClaim{}, false, err
	}
	encoded, inputs, claimed, err := g.store.pendingInputState(id)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	if claimed || len(inputs) == 0 || inputs[0] != expected {
		return DeliveryClaim{}, false, nil
	}
	token, err := newDeliveryToken()
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	res, err := g.store.db.Exec(`
UPDATE sessions
   SET pending_claimed = 1, pending_claimed_at = ?, pending_attempt_token = ?,
       pending_receipt_token = '', pending_delivery_outcome = ?
 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`,
		encodeTime(at), token, DeliveryInFlight, id, encoded)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		return DeliveryClaim{}, false, err
	}
	return DeliveryClaim{AttemptToken: token, ClaimedAt: at}, true, nil
}

func (g *DeliveryGuard) FinishPendingInput(id, expected string, claim DeliveryClaim, outcome DeliveryOutcome, at time.Time) error {
	if err := g.requireActive(); err != nil {
		return err
	}
	if claim.AttemptToken == "" || !terminalDeliveryOutcome(outcome) {
		return ErrDeliveryReceiptRejected
	}
	encoded, inputs, claimed, err := g.store.pendingInputState(id)
	if err != nil {
		return err
	}
	if !claimed || len(inputs) == 0 || inputs[0] != expected {
		return ErrDeliveryReceiptRejected
	}
	remaining, err := encodePendingInputs(inputs[1:])
	if err != nil {
		return err
	}
	res, err := g.store.db.Exec(`
UPDATE sessions
   SET pending_inputs = ?, pending_claimed = 0, pending_receipt_token = ?,
       pending_delivery_outcome = ?
 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1 AND pending_attempt_token = ?`,
		remaining, claim.AttemptToken, outcome, id, encoded, claim.AttemptToken)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrDeliveryReceiptRejected
	}
	return nil
}

func (g *DeliveryGuard) RecoverPendingInput(id, expected string, at time.Time) error {
	if err := g.requireActive(); err != nil {
		return err
	}
	var token string
	var claimed int
	if err := g.store.db.QueryRow(
		`SELECT pending_attempt_token, pending_claimed FROM sessions WHERE id = ?`, id,
	).Scan(&token, &claimed); err != nil {
		return err
	}
	if claimed == 0 || token == "" {
		return ErrDeliveryReceiptRejected
	}
	return g.FinishPendingInput(id, expected, DeliveryClaim{AttemptToken: token}, DeliveryUncertain, at)
}
