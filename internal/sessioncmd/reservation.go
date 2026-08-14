package sessioncmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/google/uuid"
)

const (
	DefaultReservationTTL = 30 * time.Minute
	MaxReservationTTL     = 4 * time.Hour
)

type Reservation struct {
	Pattern   string `json:"pattern" jsonschema:"path or glob the lease covers"`
	Mode      string `json:"mode" jsonschema:"exclusive (nobody else edits these paths) or shared (others may read or also edit)"`
	SessionID string `json:"session_id" jsonschema:"session holding the lease"`
	Holder    string `json:"holder" jsonschema:"name of the session holding the lease"`
	Note      string `json:"note,omitempty" jsonschema:"what the holder is doing there"`
	ExpiresIn string `json:"expires_in" jsonschema:"time left before the lease lapses on its own"`
	Mine      bool   `json:"mine" jsonschema:"whether the calling session holds it"`
}

type ReserveResult struct {
	Reserved  []Reservation `json:"reserved" jsonschema:"leases this session now holds"`
	Conflicts []Reservation `json:"conflicts,omitempty" jsonschema:"live leases held by other sessions that overlap what was asked for; these are advisory, so nothing is blocked, but the holders should be messaged before editing"`
}

func normalizeMode(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", store.ReservationExclusive:
		return store.ReservationExclusive, nil
	case store.ReservationShared:
		return store.ReservationShared, nil
	}
	return "", fmt.Errorf("unknown mode %q; use exclusive or shared", mode)
}

func (r *runtime) reservationInfo(reservation store.Reservation, names map[string]string, sessionID string, now time.Time) Reservation {
	return Reservation{
		Pattern:   reservation.Pattern,
		Mode:      reservation.Mode,
		SessionID: reservation.SessionID,
		Holder:    names[reservation.SessionID],
		Note:      reservation.Note,
		ExpiresIn: reservation.ExpiresAt.Sub(now).Round(time.Minute).String(),
		Mine:      reservation.SessionID == sessionID,
	}
}

// Reserve declares that this session is working on a set of paths. The
// lease is advisory and expires on its own, so an agent that dies never
// holds the repo hostage. Overlapping leases are reported rather than
// refused: the point is that both agents find out in time to talk.
func (s *Sessions) Reserve(sessionID string, patterns []string, mode, note string, ttl time.Duration) (ReserveResult, error) {
	mode, err := normalizeMode(mode)
	if err != nil {
		return ReserveResult{}, err
	}
	cleaned := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern != "" {
			cleaned = append(cleaned, pattern)
		}
	}
	if len(cleaned) == 0 {
		return ReserveResult{}, errors.New("no paths given; pass the files or globs you are about to edit")
	}
	switch {
	case ttl < 0 || ttl > MaxReservationTTL:
		return ReserveResult{}, fmt.Errorf("ttl %s is outside 0 to %s; reserve again when the lease lapses", ttl, MaxReservationTTL)
	case ttl == 0:
		ttl = DefaultReservationTTL
	}
	runtime, err := s.open()
	if err != nil {
		return ReserveResult{}, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return ReserveResult{}, err
	}
	names, err := runtime.sessionNames()
	if err != nil {
		return ReserveResult{}, err
	}
	now := time.Now()
	result := ReserveResult{Reserved: make([]Reservation, 0, len(cleaned))}
	for _, pattern := range cleaned {
		clashes, err := runtime.store.Conflicts(caller.ID, pattern, mode, now)
		if err != nil {
			return ReserveResult{}, err
		}
		for _, clash := range clashes {
			result.Conflicts = append(result.Conflicts, runtime.reservationInfo(clash, names, caller.ID, now))
		}
		reservation := store.Reservation{
			ID:         uuid.NewString()[:8],
			SessionID:  caller.ID,
			Pattern:    pattern,
			Mode:       mode,
			Note:       strings.TrimSpace(note),
			AcquiredAt: now,
			ExpiresAt:  now.Add(ttl),
		}
		if err := runtime.store.Reserve(reservation); err != nil {
			return ReserveResult{}, err
		}
		result.Reserved = append(result.Reserved, runtime.reservationInfo(reservation, names, caller.ID, now))
	}
	return result, nil
}

// ReleaseFiles drops this session's leases once it is done editing.
func (s *Sessions) ReleaseFiles(sessionID string, patterns []string) (int, error) {
	runtime, err := s.open()
	if err != nil {
		return 0, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return 0, err
	}
	cleaned := make([]string, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern = strings.TrimSpace(pattern); pattern != "" {
			cleaned = append(cleaned, pattern)
		}
	}
	// Releasing everything is what an omitted list means, never what a list
	// that turned out to hold only blanks means.
	if len(patterns) > 0 && len(cleaned) == 0 {
		return 0, errors.New("no usable paths given; omit paths entirely to release every lease this session holds")
	}
	if len(cleaned) == 0 {
		released, err := runtime.store.Release(caller.ID, "")
		return int(released), err
	}
	total := int64(0)
	for _, pattern := range cleaned {
		released, err := runtime.store.Release(caller.ID, pattern)
		if err != nil {
			return int(total), err
		}
		total += released
	}
	return int(total), nil
}

// Reservations lists every live lease, so an agent can see what the rest
// of the fleet is editing before it starts.
func (s *Sessions) Reservations(sessionID string) ([]Reservation, error) {
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.store.Close()
	caller, err := runtime.caller(sessionID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := runtime.store.PruneReservations(now); err != nil {
		return nil, err
	}
	stored, err := runtime.store.Reservations(now)
	if err != nil {
		return nil, err
	}
	names, err := runtime.sessionNames()
	if err != nil {
		return nil, err
	}
	reservations := make([]Reservation, 0, len(stored))
	for _, reservation := range stored {
		reservations = append(reservations, runtime.reservationInfo(reservation, names, caller.ID, now))
	}
	return reservations, nil
}
