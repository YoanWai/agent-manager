package sessioncmd

// SnapshotVersion is the envelope shape another manager reads over SSH. A
// change a reader of the previous shape would misread bumps it.
const SnapshotVersion = 1

// Snapshot is one host's inventory as another manager sees it: every row
// in list order, its groups, and whether a manager is awake to deliver
// queued messages there.
type Snapshot struct {
	Version      int        `json:"version"`
	ManagerAwake bool       `json:"manager_awake"`
	Sessions     []Session  `json:"sessions"`
	Terminals    []Terminal `json:"terminals"`
	Groups       []Group    `json:"groups"`
}
