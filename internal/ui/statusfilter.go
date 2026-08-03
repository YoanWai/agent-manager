package ui

import "github.com/YoanWai/agent-manager/internal/status"

// statusFilter is which status set the list shows. The zero value is all
// sessions. To add a mode: define a const, append it to statusFilterCycle,
// and handle it in label and matches.
type statusFilter int

const (
	statusFilterAll statusFilter = iota
	statusFilterAttention
)

// statusFilterCycle is the order `w` walks. Append new modes before the
// wrap back to all happens at the end of next().
var statusFilterCycle = []statusFilter{
	statusFilterAll,
	statusFilterAttention,
}

func (f statusFilter) next() statusFilter {
	for i, mode := range statusFilterCycle {
		if mode == f {
			return statusFilterCycle[(i+1)%len(statusFilterCycle)]
		}
	}
	return statusFilterAll
}

func (f statusFilter) active() bool {
	return f != statusFilterAll
}

// label is the short badge word for the header and empty state, empty when
// the filter shows every status.
func (f statusFilter) label() string {
	switch f {
	case statusFilterAttention:
		return "attention"
	default:
		return ""
	}
}

// matches reports whether a session status is visible under this filter.
func (f statusFilter) matches(st string) bool {
	switch f {
	case statusFilterAttention:
		switch st {
		case status.Waiting, status.Finished, status.Errored:
			return true
		default:
			return false
		}
	default:
		return true
	}
}
