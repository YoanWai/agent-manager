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
// one's list. internal/remote validates both fields; the store keeps them.
type Connection struct {
	Name        string
	Destination string
}

func (s *Store) Connections() ([]Connection, error) {
	rows, err := s.db.Query(`SELECT name, destination FROM connections ORDER BY position, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var connections []Connection
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.Name, &c.Destination); err != nil {
			return nil, err
		}
		connections = append(connections, c)
	}
	return connections, rows.Err()
}

// AddConnection appends a connection after every existing one.
func (s *Store) AddConnection(c Connection) error {
	res, err := s.db.Exec(
		`INSERT INTO connections (name, destination, position, created_at)
		 VALUES (?, ?, (SELECT COALESCE(MAX(position)+1, 0) FROM connections), ?)
		 ON CONFLICT(name) DO NOTHING`, c.Name, c.Destination, encodeTime(time.Now()))
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
// destination, keeping its place in the list.
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
	res, err := tx.Exec(`UPDATE connections SET name = ?, destination = ? WHERE name = ?`,
		next.Name, next.Destination, name)
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
	return tx.Commit()
}

func (s *Store) DeleteConnection(name string) error {
	res, err := s.db.Exec(`DELETE FROM connections WHERE name = ?`, name)
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
	return nil
}
