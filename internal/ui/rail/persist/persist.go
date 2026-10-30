// Package persist is the Rail's runtime: the folds a previous run left
// folded, the store writes a collapse, swap, placement or group move makes,
// and the pane labels those writes leave stale. The Rail model in package
// rail decides; the root queues the decision here.
package persist

import (
	"encoding/json"
	"sort"

	"github.com/YoanWai/agent-manager/internal/store"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

const (
	CollapsedSetting = "collapsed_groups"
	// HostCollapsedSetting holds the folds on connection rows, kept apart
	// from local paths because a local group may be named like a host row.
	HostCollapsedSetting = "collapsed_connection_groups"
)

type Store interface {
	Get(id string) (store.Session, error)
	Setting(key string) (string, error)
	SetSetting(key, value string) error
	SwapSessionOrder(id, targetID string) error
	SwapGroupOrder(path, targetPath string, siblingOrder ...string) error
	PlaceSession(id, group, parentID string) error
	PlaceSessionBefore(id, beforeID string) error
	MoveGroup(path, newParent string) error
	Children(parentID string) ([]store.Session, error)
	SessionsInSubtree(path string) ([]store.Session, error)
}

type Labels interface {
	Exists(id string) bool
	SetLabel(id, label string) error
}

func Load(st Store) uirail.Model {
	rail := uirail.New(Collapsed(st))
	raw, err := st.Setting(HostCollapsedSetting)
	if err != nil || raw == "" {
		return rail
	}
	var folds []uirail.HostFold
	if json.Unmarshal([]byte(raw), &folds) == nil {
		rail.RestoreHostFolds(folds)
	}
	return rail
}

func Collapsed(st Store) []string {
	raw, err := st.Setting(CollapsedSetting)
	if err != nil || raw == "" {
		return nil
	}
	var paths []string
	if json.Unmarshal([]byte(raw), &paths) != nil {
		return nil
	}
	return paths
}

// SaveFolds writes both fold sets, so a save keeps the whole picture the
// user sees rather than only the half that changed.
func SaveFolds(st Store, collapsed []string, hostFolds []uirail.HostFold) error {
	sort.Strings(collapsed)
	raw, err := json.Marshal(collapsed)
	if err != nil {
		return err
	}
	if err := st.SetSetting(CollapsedSetting, string(raw)); err != nil {
		return err
	}
	raw, err = json.Marshal(hostFolds)
	if err != nil {
		return err
	}
	return st.SetSetting(HostCollapsedSetting, string(raw))
}
