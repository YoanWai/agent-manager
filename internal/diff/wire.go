package diff

import (
	"encoding/json"
	"errors"
)

// The loading flags and diagnostic are part of a remote review snapshot too.
func (f FileDiff) MarshalJSON() ([]byte, error) {
	type plain FileDiff
	copy := f
	copy.Err = nil
	diagnostic := ""
	if f.Err != nil {
		diagnostic = f.Err.Error()
	}
	return json.Marshal(struct {
		plain
		Loaded    bool
		StatKnown bool
		Error     string
	}{plain(copy), f.loaded, f.statKnown, diagnostic})
}

func (f *FileDiff) UnmarshalJSON(data []byte) error {
	type plain FileDiff
	var wire struct {
		plain
		Loaded    bool
		StatKnown bool
		Error     string
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*f = FileDiff(wire.plain)
	f.loaded, f.statKnown = wire.Loaded, wire.StatKnown
	if wire.Error != "" {
		f.Err = errors.New(wire.Error)
	}
	return nil
}
