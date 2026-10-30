package persist

import (
	"fmt"

	"github.com/YoanWai/agent-manager/internal/keybind"
	tea "github.com/charmbracelet/bubbletea"
)

// KeySaver writes one key table; tests script a failing write through it.
type KeySaver func(table keybind.Table) error

// KeysRequest is the key picker's tables at dispatch, with which of them the
// picker changed.
type KeysRequest struct {
	List, Session  keybind.Table
	ListChanged    bool
	SessionChanged bool
}

// KeysResult reports which tables landed; a partial failure commits the
// first and stops.
type KeysResult struct {
	ListCommitted    bool
	SessionCommitted bool
}

type KeysRoot interface {
	ReportErr(text string)
	SaveKeys(list, session keybind.Table, listChanged, sessionChanged bool) tea.Cmd
}

func RunKeys(request KeysRequest, save KeySaver) (KeysResult, error) {
	result := KeysResult{}
	tot := 0
	if request.ListChanged {
		tot++
	}
	if request.SessionChanged {
		tot++
	}
	committed := 0
	if request.ListChanged {
		if err := save(request.List); err != nil {
			return result, fmt.Errorf("key save committed %d of %d writes: %w", committed, tot, err)
		}
		result.ListCommitted = true
		committed++
	}
	if request.SessionChanged {
		if err := save(request.Session); err != nil {
			return result, fmt.Errorf("key save committed %d of %d writes: %w", committed, tot, err)
		}
		result.SessionCommitted = true
	}
	return result, nil
}

func ApplyKeys(root KeysRoot, request KeysRequest, result KeysResult, err error) tea.Cmd {
	cmd := root.SaveKeys(request.List, request.Session, result.ListCommitted, result.SessionCommitted)
	if err != nil {
		root.ReportErr(err.Error())
		return nil
	}
	return cmd
}

// Expected is the tables the store will hold once the queued key saves
// land, running first, so a picker submits only what is still different.
func Expected(list, session keybind.Table, queued []KeysRequest) (keybind.Table, keybind.Table) {
	for _, pending := range queued {
		if pending.ListChanged {
			list = pending.List
		}
		if pending.SessionChanged {
			session = pending.Session
		}
	}
	return list, session
}
