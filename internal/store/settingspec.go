package store

import (
	"fmt"
	"slices"
	"strings"
)

// SettingSpec describes one key of the settings table: the values it
// takes, so the shell and the Settings modal write the same ones.
type SettingSpec struct {
	Key     string
	Values  []string
	Default string
	// List marks a key holding a comma-separated subset of Values.
	List bool
	// Row is the label of the Settings row that steps this key, empty for a
	// key the shell alone sets.
	Row string
}

// Check reports whether value is one the key takes. A key with no Values
// takes any text.
func (s SettingSpec) Check(value string) error {
	if len(s.Values) == 0 {
		return nil
	}
	if !s.List {
		return s.checkOne(value)
	}
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			if err := s.checkOne(part); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s SettingSpec) checkOne(value string) error {
	if slices.Contains(s.Values, value) {
		return nil
	}
	return fmt.Errorf("%s does not take %q; it takes %s", s.Key, value, strings.Join(s.Values, ", "))
}
