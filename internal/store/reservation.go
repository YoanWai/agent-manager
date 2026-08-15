package store

import (
	"database/sql"
	"time"
)

const (
	ReservationExclusive = "exclusive"
	ReservationShared    = "shared"
)

// Reservation is an advisory lease over a set of paths. It is not a lock:
// nothing stops an agent editing a reserved file. Its job is to surface
// the collision while both agents can still talk about it, which is what
// a worktree cannot do, since worktrees defer the conflict to merge time.
type Reservation struct {
	ID         string
	SessionID  string
	Pattern    string
	Mode       string
	Note       string
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

// Reserve takes or extends this session's lease on a pattern and returns the
// live leases held by others that overlap it. Reading the overlap and taking
// the lease is one transaction, because two agents reserving the same path at
// once would otherwise both read the list before either wrote and neither
// would hear about the other, which is the only thing the lease is for. A
// session renewing its own lease is not a conflict with itself.
func (s *Store) Reserve(reservation Reservation) ([]Reservation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	conflicts, err := overlappingReservations(tx, reservation)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`
INSERT INTO file_reservations (id, session_id, pattern, mode, note, acquired_at, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, pattern) DO UPDATE SET
	mode = excluded.mode, note = excluded.note, expires_at = excluded.expires_at`,
		reservation.ID, reservation.SessionID, reservation.Pattern, reservation.Mode,
		reservation.Note, encodeTime(reservation.AcquiredAt), encodeTime(reservation.ExpiresAt)); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return conflicts, nil
}

// overlappingReservations finds live leases over the same paths held by
// someone else. It takes the transaction because the store holds a single
// connection: a query issued on s.db while a Tx holds it waits forever.
//
// Comparing with GLOB in both directions catches a pattern against a literal
// path either way round; two patterns that both contain wildcards are not
// comparable in SQL and are reported only on an exact match, which is why
// these are advisory.
func overlappingReservations(tx *sql.Tx, reservation Reservation) ([]Reservation, error) {
	rows, err := tx.Query(`
SELECT id, session_id, pattern, mode, note, acquired_at, expires_at
  FROM file_reservations
 WHERE session_id != ? AND expires_at > ?
   AND (pattern = ? OR pattern GLOB ? OR ? GLOB pattern)
   AND (mode = ? OR ? = ?)
 ORDER BY acquired_at`,
		reservation.SessionID, encodeTime(reservation.AcquiredAt),
		reservation.Pattern, reservation.Pattern, reservation.Pattern,
		ReservationExclusive, reservation.Mode, ReservationExclusive)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReservations(rows)
}

// Release drops one pattern's lease, or every lease this session holds
// when the pattern is empty.
func (s *Store) Release(sessionID, pattern string) (int64, error) {
	query := `DELETE FROM file_reservations WHERE session_id = ?`
	args := []any{sessionID}
	if pattern != "" {
		query += ` AND pattern = ?`
		args = append(args, pattern)
	}
	res, err := s.db.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Reservations lists every lease still live at now. An expired lease is
// left in place for the next sweep rather than hidden behind a delete,
// so a crashed agent's claim lapses instead of holding the repo.
func (s *Store) Reservations(now time.Time) ([]Reservation, error) {
	rows, err := s.db.Query(`
SELECT id, session_id, pattern, mode, note, acquired_at, expires_at
  FROM file_reservations WHERE expires_at > ? ORDER BY acquired_at, pattern`, encodeTime(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanReservations(rows)
}

func (s *Store) PruneReservations(now time.Time) error {
	_, err := s.db.Exec(`DELETE FROM file_reservations WHERE expires_at <= ?`, encodeTime(now))
	return err
}

func scanReservations(rows rowScanner) ([]Reservation, error) {
	reservations := make([]Reservation, 0)
	for rows.Next() {
		var reservation Reservation
		var acquiredAt, expiresAt int64
		if err := rows.Scan(&reservation.ID, &reservation.SessionID, &reservation.Pattern,
			&reservation.Mode, &reservation.Note, &acquiredAt, &expiresAt); err != nil {
			return nil, err
		}
		reservation.AcquiredAt = decodeTime(acquiredAt)
		reservation.ExpiresAt = decodeTime(expiresAt)
		reservations = append(reservations, reservation)
	}
	return reservations, rows.Err()
}

type rowScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}
