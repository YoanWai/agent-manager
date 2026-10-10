package focus

const scrollStep = 3

// RegionRequest asks the root watcher adapter for one scrollback viewport.
type RegionRequest struct {
	SessionID string
	Offset    int
	Rows      int
}

type RegionResult struct {
	SessionID string
	Offset    int
	Rows      int
	Preview   string
	OK        bool
}

type RegionOutcome struct {
	Apply   bool
	Preview string
	Next    *RegionRequest
}

// Scroll moves the target by wheel delta. Negative delta moves into history,
// matching the existing focused-pane wheel convention.
func (m *Model) Scroll(delta int, sessionID string, rows int) *RegionRequest {
	return m.ScrollLines(delta*scrollStep, sessionID, rows)
}

// ScrollLines moves the target by whole lines, the step page keys take.
func (m *Model) ScrollLines(lines int, sessionID string, rows int) *RegionRequest {
	offset := m.scroll - lines
	offset = min(max(offset, 0), m.pane.history)
	if offset == m.scroll {
		return nil
	}
	m.scroll = offset
	return m.requestRegion(sessionID, rows)
}

func (m *Model) requestRegion(sessionID string, rows int) *RegionRequest {
	if m.fetchInFlight {
		return nil
	}
	m.fetchInFlight = true
	return &RegionRequest{SessionID: sessionID, Offset: m.scroll, Rows: rows}
}

func (m *Model) ApplyRegion(result RegionResult, currentSessionID string, currentRows int) RegionOutcome {
	if currentSessionID == "" || result.SessionID != currentSessionID {
		m.fetchInFlight = false
		return RegionOutcome{}
	}
	if result.Offset != m.scroll || result.Rows != currentRows {
		return RegionOutcome{Next: &RegionRequest{
			SessionID: currentSessionID,
			Offset:    m.scroll,
			Rows:      currentRows,
		}}
	}
	m.fetchInFlight = false
	if !result.OK {
		return RegionOutcome{}
	}
	return RegionOutcome{Apply: true, Preview: result.Preview}
}
