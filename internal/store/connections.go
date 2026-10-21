package store

import (
	"errors"
	"fmt"
	"time"
)

// ErrConnectionTaken reports a connection name another connection holds.
var ErrConnectionTaken = errors.New("connection name is taken")

// ErrConnectionNotFound reports a connection name no row carries.
var ErrConnectionNotFound = errors.New("connection does not exist")

// Connection is an SSH destination whose manager's sessions show in this
// one's list. internal/remote validates its fields; the store keeps them.
type Connection struct {
	Name        string
	Destination string
	Binary      string
}

func (s *Store) Connections() ([]Connection, error) {
	rows, err := s.db.Query(`SELECT name, destination, binary FROM connections ORDER BY position, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var connections []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.Name, &c.Destination, &c.Binary); err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	return connections, rows.Err()
}

// AddConnection appends a connection after every existing one.
func (s *Store) AddConnection(c Connection) error {
	res, err := s.db.Exec(
		`INSERT INTO connections (name, destination, binary, position, created_at)
		 VALUES (?, ?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM connections), ?)
		 ON CONFLICT(name) DO NOTHING`, c.Name, c.Destination, c.Binary, encodeTime(time.Now()))
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("connection %q: %w", c.Name, ErrConnectionTaken)
	}
	return nil
}

// UpdateConnection renames a connection and/or points it at a new
// destination or path, keeping its place in the list.
func (s *Store) UpdateConnection(name string, next Connection) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if next.Name != name {
		var taken int
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM connections WHERE name = ?)`, next.Name).Scan(&taken); err != nil {
			return err
		}
		if taken == 1 {
			return fmt.Errorf("connection %q: %w", next.Name, ErrConnectionTaken)
		}
	}
	res, err := tx.Exec(`UPDATE connections SET name = ?, destination = ?, binary = ? WHERE name = ?`,
		next.Name, next.Destination, next.Binary, name)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("connection %q: %w", name, ErrConnectionNotFound)
	}
	// The sessions that opened terminals there keep them under the new name.
	if _, err := tx.Exec(`UPDATE remote_terminals SET host = ? WHERE host = ?`, next.Name, name); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteConnection drops the connection and who opened its terminals, so a
// later connection by that name starts with none.
func (s *Store) DeleteConnection(name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM connections WHERE name = ?`, name)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("connection %q: %w", name, ErrConnectionNotFound)
	}
	if _, err := tx.Exec(`DELETE FROM remote_terminals WHERE host = ?`, name); err != nil {
		return err
	}
	return tx.Commit()
}
