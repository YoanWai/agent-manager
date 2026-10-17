package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func (s *Store) ClaimPendingInput(id, expected string) (bool, error) {
	return false, ErrDeliveryGuardRequired
}

func (s *Store) ConsumeClaimedPendingInput(id, expected string) (bool, error) {
	return false, ErrDeliveryGuardRequired
}

func (s *Store) pendingInputState(id string) (string, []string, bool, error) {
	var encoded string
	var claimed int
	if err := s.db.QueryRow(
		`SELECT pending_inputs, pending_claimed FROM sessions WHERE id = ?`, id,
	).Scan(&encoded, &claimed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil, false, fmt.Errorf("session %s: %w", id, ErrSessionGone)
		}
		return "", nil, false, err
	}
	var inputs []string
	if err := json.Unmarshal([]byte(encoded), &inputs); err != nil {
		return "", nil, false, fmt.Errorf("decode pending inputs for session %s: %w", id, err)
	}
	return encoded, inputs, claimed != 0, nil
}

func encodePendingInputs(inputs []string) (string, error) {
	if len(inputs) == 0 {
		return "[]", nil
	}
	encoded, err := json.Marshal(inputs)
	return string(encoded), err
}
