package ui

type focusRuntimeState struct {
	watch             *focusWatch
	imeCursor         *cursorAnchor
	lastPaneSizes     map[string][2]int
	lastPanePIDs      map[string]int
	lastPublishedSize [2]int
}
