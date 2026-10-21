package sessioncmd

import "time"

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

// Snapshot lists this host's sessions and terminals, archived ones too, from
// a single read of the sessions table, so a terminal never names a parent
// row the same snapshot lacks.
func (s *Sessions) Snapshot(sessionID string) (Snapshot, error) {
	runtime, err := s.open()
	if err != nil {
		return Snapshot{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return Snapshot{}, err
	}
	stored, err := runtime.store.ListSessions(true)
	if err != nil {
		return Snapshot{}, err
	}
	groups, err := runtime.store.Groups()
	if err != nil {
		return Snapshot{}, err
	}
	panes, err := runtime.driver.Panes()
	if err != nil {
		return Snapshot{}, err
	}
	awake, err := runtime.managerAwake(time.Now())
	if err != nil {
		return Snapshot{}, err
	}
	names := make(map[string]string, len(stored))
	for _, sess := range stored {
		names[sess.ID] = sess.Name
	}
	snapshot := Snapshot{
		Version:      SnapshotVersion,
		ManagerAwake: awake,
		Sessions:     make([]Session, 0),
		Terminals:    make([]Terminal, 0),
		Groups:       runtime.groupRows(groups, stored),
	}
	for _, sess := range stored {
		_, running := panes[sess.ID]
		if runtime.cfg.Tools[sess.Tool].Shell {
			snapshot.Terminals = append(snapshot.Terminals, runtime.terminalInfo(sess, running, names[sess.ParentID]))
			continue
		}
		snapshot.Sessions = append(snapshot.Sessions, runtime.sessionInfo(sess, running, sess.ID == sessionID))
	}
	return snapshot, nil
}
