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

// KeysRoot is what a key save completion asks of the manager.
type KeysRoot interface {
	ReportErr(text string)
	SaveKeys(list, session keybind.Table, listChanged, sessionChanged bool) tea.Cmd
}

// RunKeys writes the changed tables in list-then-session order.
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

// ApplyKeys commits what landed and reports a failure that stopped the rest.
func ApplyKeys(root KeysRoot, request KeysRequest, result KeysResult, err error) tea.Cmd {
	cmd := root.SaveKeys(request.List, request.Session, result.ListCommitted, result.SessionCommitted)
	if err != nil {
		root.ReportErr(err.Error())
		return nil
	}
	return cmd
}
