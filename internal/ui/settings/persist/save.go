package persist

import (
	"fmt"
	"slices"
	"strings"

	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

// SaveRequest is the dialog's chosen preferences and CLI visibility as the
// save captured them. A nil Hidden means the dialog did not touch the
// hidden-CLI set; an empty one means it cleared it.
type SaveRequest struct {
	Values []uisettings.Value
	Hidden []string
	// StagedUpdate says the save must land before the self-update it was
	// closed for starts.
	StagedUpdate bool
	Generation   uint64
}

// Clone copies the mutable slices so a later dialog edit never retargets an
// accepted save.
func (r SaveRequest) Clone() SaveRequest {
	r.Values = slices.Clone(r.Values)
	r.Hidden = slices.Clone(r.Hidden)
	return r
}

// SaveResult reports what the worker durably committed. On a partial
// failure Restored carries the read-back of the committed keys with a
// per-key read error: a failed read must not be presented as the committed
// value. A nil Restored means every write committed.
type SaveResult struct {
	Generation     uint64
	Committed, Tot int
	Restored       []uisettings.Readback
	RestoredHidden string
	HiddenErr      error
}

// RunSave writes the captured values in the order the dialog listed them.
func RunSave(request SaveRequest, writer Writer) (SaveResult, error) {
	result := SaveResult{Generation: request.Generation}
	result.Tot = len(request.Values)
	if request.Hidden != nil {
		result.Tot++
	}
	for _, value := range request.Values {
		var err error
		if value.Proactive {
			err = writer.SetProactiveCoordination(value.Value == "on")
		} else {
			err = writer.SetSetting(value.Key, value.Value)
		}
		if err != nil {
			result.Restored, result.RestoredHidden, result.HiddenErr = SettingsReadback(writer)
			return result, fmt.Errorf("settings save committed %d of %d writes: %w", result.Committed, result.Tot, err)
		}
		result.Committed++
	}
	if request.Hidden != nil {
		if err := writer.SetSetting(uisettings.HiddenToolsSetting, strings.Join(request.Hidden, ",")); err != nil {
			result.Restored, result.RestoredHidden, result.HiddenErr = SettingsReadback(writer)
			return result, fmt.Errorf("settings save committed %d of %d writes: %w", result.Committed, result.Tot, err)
		}
		result.Committed++
	}
	return result, nil
}

// ApplySave reconciles the worker's durable outcome. Prefs are runtime
// state, not dialog state: a failure's committed read-back reconciles them
// on every completion, stale included, because the lane's FIFO order lets a
// later save's completion win. Dialog fields are restored only while the
// request is the latest accepted save, so a newer dialog is never replaced.
func (s *State) ApplySave(root Root, request SaveRequest, result SaveResult, err error) tea.Cmd {
	if s.Pending > 0 {
		s.Pending--
	}
	stale := s.Gen != request.Generation
	if err != nil {
		root.ReportErr(err.Error())
		if result.Restored != nil {
			s.Cache.ApplyReadback(result.Restored, result.RestoredHidden, result.HiddenErr)
			if note := root.MirrorPrefs(result.Restored); note != "" {
				root.Caveat(note)
			}
			if !stale {
				root.Restore(result.Restored, result.RestoredHidden, result.HiddenErr, s.Cache.Editors())
			}
		}
	}
	if err == nil {
		s.Cache.ApplyValues(request.Values)
		if request.Hidden != nil {
			s.Cache.ApplyHidden(request.Hidden)
		}
		committed := make([]uisettings.Readback, 0, len(request.Values))
		for _, value := range request.Values {
			committed = append(committed, uisettings.Readback{Key: value.Key, Value: value.Value})
		}
		root.MirrorPrefs(committed)
	}
	if request.StagedUpdate {
		return root.Update(err == nil)
	}
	return nil
}
