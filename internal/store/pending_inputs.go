package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

func (s *Store) ClaimPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_claimed = 1
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 0`, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
}

func (s *Store) ConsumeClaimedPendingInput(id, expected string) (bool, error) {
	encoded, inputs, claimed, err := s.pendingInputState(id)
	if err != nil {
		return false, err
	}
	if !claimed || len(inputs) == 0 || inputs[0] != expected {
		return false, nil
	}
	remaining, err := encodePendingInputs(inputs[1:])
	if err != nil {
		return false, err
	}
	res, err := s.db.Exec(
		`UPDATE sessions SET pending_inputs = ?, pending_claimed = 0
		 WHERE id = ? AND pending_inputs = ? AND pending_claimed = 1`,
		remaining, id, encoded)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, s.requireRowOrNoop(res, id)
	}
	return true, nil
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
