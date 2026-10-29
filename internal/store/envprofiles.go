package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"unicode"
	"unicode/utf8"
)

// EnvProfile is a named set of environment variables a session launches
// under, on top of the environment the manager itself runs in.
type EnvProfile struct {
	Name string
	Vars map[string]string
}

// reservedEnvPrefix belongs to the variables the manager sets on every
// launch, which a profile must not shadow.
const reservedEnvPrefix = "AGENT_MANAGER_"

// CheckEnvProfileName refuses a name the CLI flag and the pickers cannot
// carry.
func CheckEnvProfileName(name string) error {
	if !utf8.ValidString(name) || strings.ContainsFunc(name, unicode.IsControl) {
		return errors.New("that name cannot be saved")
	}
	if name != strings.TrimSpace(name) || name == "" || utf8.RuneCountInString(name) > 60 {
		return errors.New("name needs 1 to 60 characters, without leading or trailing space")
	}
	return nil
}

// CheckEnvKey refuses a key a POSIX shell cannot export, and the
// manager's own.
func CheckEnvKey(key string) error {
	if key == "" {
		return errors.New("variable name is empty")
	}
	for i, r := range key {
		letter := r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
		if !letter && (i == 0 || r < '0' || r > '9') {
			return fmt.Errorf("variable name %q takes letters, digits and _, and cannot start with a digit", key)
		}
	}
	if strings.HasPrefix(strings.ToUpper(key), reservedEnvPrefix) {
		return fmt.Errorf("variable %s is set by agent-manager itself", key)
	}
	return nil
}

func (s *Store) EnvProfiles() ([]EnvProfile, error) {
	rows, err := s.db.Query(`SELECT name, vars FROM env_profiles ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	profiles := make([]EnvProfile, 0)
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		profile, err := decodeEnvProfile(name, raw)
		if err != nil {
			return nil, err
		}
		profiles = append(profiles, profile)
	}
	return profiles, rows.Err()
}

func (s *Store) EnvProfile(name string) (EnvProfile, bool, error) {
	return envProfile(s.db, name)
}

type queryRower interface {
	QueryRow(query string, args ...any) *sql.Row
}

func envProfile(db queryRower, name string) (EnvProfile, bool, error) {
	var raw string
	err := db.QueryRow(`SELECT vars FROM env_profiles WHERE name = ?`, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return EnvProfile{}, false, nil
	}
	if err != nil {
		return EnvProfile{}, false, err
	}
	profile, err := decodeEnvProfile(name, raw)
	return profile, err == nil, err
}

func decodeEnvProfile(name, raw string) (EnvProfile, error) {
	profile := EnvProfile{Name: name, Vars: map[string]string{}}
	if err := json.Unmarshal([]byte(raw), &profile.Vars); err != nil {
		return EnvProfile{}, fmt.Errorf("decode environment %s: %w", name, err)
	}
	return profile, nil
}

// SetEnvVars sets vars on the named profile, creating it when it is new.
func (s *Store) SetEnvVars(name string, vars map[string]string) (EnvProfile, error) {
	if err := CheckEnvProfileName(name); err != nil {
		return EnvProfile{}, err
	}
	if len(vars) == 0 {
		return EnvProfile{}, errors.New("name at least one KEY=VALUE to set")
	}
	for key, value := range vars {
		if err := CheckEnvKey(key); err != nil {
			return EnvProfile{}, err
		}
		if strings.ContainsRune(value, 0) {
			return EnvProfile{}, fmt.Errorf("value of %s holds a NUL byte", key)
		}
	}
	return s.updateEnvProfile(name, true, func(current map[string]string) { maps.Copy(current, vars) })
}

// UnsetEnvVars removes keys from the named profile, which stays defined.
func (s *Store) UnsetEnvVars(name string, keys []string) (EnvProfile, error) {
	if len(keys) == 0 {
		return EnvProfile{}, errors.New("name at least one variable to unset")
	}
	return s.updateEnvProfile(name, false, func(current map[string]string) {
		for _, key := range keys {
			delete(current, key)
		}
	})
}

func (s *Store) updateEnvProfile(name string, create bool, change func(map[string]string)) (EnvProfile, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return EnvProfile{}, err
	}
	defer tx.Rollback()
	profile, found, err := envProfile(tx, name)
	if err != nil {
		return EnvProfile{}, err
	}
	if !found && !create {
		return EnvProfile{}, fmt.Errorf("environment %q does not exist", name)
	}
	if !found {
		profile = EnvProfile{Name: name, Vars: map[string]string{}}
	}
	change(profile.Vars)
	raw, err := json.Marshal(profile.Vars)
	if err != nil {
		return EnvProfile{}, err
	}
	if _, err := tx.Exec(
		`INSERT INTO env_profiles (name, vars) VALUES (?, ?)
		 ON CONFLICT(name) DO UPDATE SET vars = excluded.vars`, name, string(raw)); err != nil {
		return EnvProfile{}, err
	}
	return profile, tx.Commit()
}

func (s *Store) DeleteEnvProfile(name string) (bool, error) {
	result, err := s.db.Exec(`DELETE FROM env_profiles WHERE name = ?`, name)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	return affected > 0, err
}

// LaunchEnv is what a session naming environment exports: none for "",
// and an error for a profile that no longer exists, so the session never
// runs under the wrong account or config.
func (s *Store) LaunchEnv(environment string) (map[string]string, error) {
	if environment == "" {
		return nil, nil
	}
	profile, found, err := s.EnvProfile(environment)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("environment %q no longer exists; recreate it with agent-manager env set", environment)
	}
	return profile.Vars, nil
}
