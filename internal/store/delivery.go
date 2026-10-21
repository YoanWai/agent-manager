package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// DeliveryOutcome is the receipt an automatic delivery leaves on its row. A
// manager older than the guard writes none, so an empty outcome beside a
// delivered_at stamp is that manager's delivered or dropped.
type DeliveryOutcome string

const (
	DeliveryInFlight  DeliveryOutcome = "in_flight"
	DeliveryConfirmed DeliveryOutcome = "confirmed"
	DeliveryRefused   DeliveryOutcome = "refused"
	// DeliveryUncertain retires a claim whose owner is gone. The paste may
	// have landed, so the row leaves the queue with delivered_at stamped and
	// no drop mark: an older reader shows it delivered, and a sender told
	// "dropped" would resend what the agent may already be doing.
	DeliveryUncertain DeliveryOutcome = "uncertain"
)

var (
	ErrDeliveryGuardRequired   = errors.New("automatic delivery write requires the profile delivery guard")
	ErrDeliveryReceiptRejected = errors.New("delivery receipt does not match the admitted attempt")
)

// DeliveryClaim is the token a claim mints; the receipt must carry it back.
type DeliveryClaim struct {
	AttemptToken string
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
	if !g.active {
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

// pendingClaimToken names the queue the claim was issued against as well.
// A manager older than the guard consumes a claimed head and claims the
// next without touching the token, so a reader tells its claim from a
// guarded one by whether the token still fits the queue.
func pendingClaimToken(encoded string) (string, error) {
	token, err := newDeliveryToken()
	if err != nil {
		return "", err
	}
	return token + "@" + queueFingerprint(encoded), nil
}

func pendingTokenIssuedFor(token, encoded string) bool {
	_, fingerprint, found := strings.Cut(token, "@")
	return found && fingerprint == queueFingerprint(encoded)
}

func queueFingerprint(encoded string) string {
	sum := sha256.Sum256([]byte(encoded))
	return hex.EncodeToString(sum[:6])
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

// ClaimMessage takes a queued message before it is typed. The same
// compare-and-set an older manager runs keeps the two from claiming one
// row; the token is what tells this claim from that manager's afterwards.
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
   SET claimed_at = ?, attempt_token = ?, delivery_outcome = ?
 WHERE id = ? AND claimed_at = 0 AND delivered_at = 0`,
		encodeTime(at), token, DeliveryInFlight, id)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		return DeliveryClaim{}, false, err
	}
	return DeliveryClaim{AttemptToken: token}, true, nil
}

// FinishMessage records the outcome of the attempt the claim admitted. A
// row another process resolved meanwhile refuses the receipt rather than
// overwrite what it wrote.
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
   SET delivered_at = ?, dropped_at = ?, delivery_outcome = ?
 WHERE id = ? AND delivered_at = 0 AND attempt_token = ? AND delivery_outcome = ?`,
		stamp, droppedAt, outcome, id, claim.AttemptToken, DeliveryInFlight)
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

// RetireMessage resolves a claim whose owner is gone as uncertain, so it is
// never typed again. It takes a guarded claim and an older manager's
// tokenless one alike; a row something else resolved first is left as is.
func (g *DeliveryGuard) RetireMessage(id int64, at time.Time) error {
	if err := g.requireActive(); err != nil {
		return err
	}
	_, err := g.store.db.Exec(`
UPDATE session_inbox
   SET delivered_at = ?, delivery_outcome = ?
 WHERE id = ? AND delivered_at = 0 AND claimed_at != 0`,
		encodeTime(at), DeliveryUncertain, id)
	return err
}

func (g *DeliveryGuard) ClaimPendingInput(id, expected string) (DeliveryClaim, bool, error) {
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
	token, err := pendingClaimToken(encoded)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	res, err := g.store.db.Exec(`
UPDATE sessions
   SET pending_claimed = 1, pending_attempt_token = ?, pending_delivery_outcome = ?
 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`,
		token, DeliveryInFlight, id, encoded)
	if err != nil {
		return DeliveryClaim{}, false, err
	}
	affected, err := res.RowsAffected()
	if err != nil || affected != 1 {
		return DeliveryClaim{}, false, err
	}
	return DeliveryClaim{AttemptToken: token}, true, nil
}

func (g *DeliveryGuard) FinishPendingInput(id, expected string, claim DeliveryClaim, outcome DeliveryOutcome) error {
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
   SET pending_inputs = ?, pending_claimed = 0, pending_attempt_token = '', pending_delivery_outcome = ?
 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1 AND pending_attempt_token = ?`,
		remaining, outcome, id, encoded, claim.AttemptToken)
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

// RetirePendingInput drops the claimed head input as uncertain, whichever
// manager claimed it, and leaves the rest of the queue to go next.
func (g *DeliveryGuard) RetirePendingInput(id, expected string) error {
	if err := g.requireActive(); err != nil {
		return err
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
	_, err = g.store.db.Exec(`
UPDATE sessions
   SET pending_inputs = ?, pending_claimed = 0, pending_attempt_token = '', pending_delivery_outcome = ?
 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1`,
		remaining, DeliveryUncertain, id, encoded)
	return err
}

// OlderManagerStamped reports whether a manager older than the guard is
// delivering for this socket: it stamped the heartbeat since a guarded
// claim last mirrored it, or a claim recorded its stamp and that stamp is
// still fresh. Claims read that once per PollerHeartbeatPeriod, and an
// older manager that starts or wakes between two of them delivers on its
// first step, so a delivery reads it again under the guard before it
// claims anything. The recorded stamp covers a second guarded manager,
// whose own claim the first one's has already written over.
func (g *DeliveryGuard) OlderManagerStamped(socket string, now time.Time) (bool, error) {
	if err := g.requireActive(); err != nil {
		return false, err
	}
	values, err := readSettings(g.store.db, PollerSocketKey, PollerHeartbeatKey, guardHeartbeatKey, olderHeartbeatKey, olderSocketKey)
	if err != nil {
		return false, err
	}
	stamp := values[PollerHeartbeatKey]
	if stamp != "" && stamp != values[guardHeartbeatKey] && values[PollerSocketKey] == socket {
		return true, nil
	}
	if values[olderSocketKey] != socket {
		return false, nil
	}
	return stampAwake(values[olderHeartbeatKey], now)
}
