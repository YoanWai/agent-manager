package link

import (
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// SaveOp is the write a connection card asked for.
type SaveOp uint8

const (
	SaveAdd SaveOp = iota + 1
	SaveUpdate
	SaveRemove
)

// SaveRequest writes the connection list. Name is the connection an update
// or removal targets; Gen is the card that asked, if any.
type SaveRequest struct {
	Op   SaveOp
	Name string
	Next store.Connection
	Gen  uint64
}

// Store is the connections table the save writes and reads back.
type Store interface {
	AddConnection(store.Connection) error
	UpdateConnection(name string, next store.Connection) error
	DeleteConnection(name string) error
	Connections() ([]store.Connection, error)
}

// Save writes the list and returns it as the store holds it.
func Save(st Store, request SaveRequest) ([]store.Connection, error) {
	var err error
	switch request.Op {
	case SaveAdd:
		err = st.AddConnection(request.Next)
	case SaveUpdate:
		err = st.UpdateConnection(request.Name, request.Next)
	case SaveRemove:
		err = st.DeleteConnection(request.Name)
	}
	if err != nil {
		return nil, err
	}
	return st.Connections()
}

// RailChange is what a saved list asks of the rail: the rename or removal
// the write made, and the connection to select.
type RailChange struct {
	Rename string
	To     string
	Forget string
	Focus  string
}

// ApplySave adopts the list the store returned after a save, or keeps the
// card open with the reason the write failed.
func (s *State) ApplySave(root Root, request SaveRequest, stored []store.Connection, err error) tea.Cmd {
	if err != nil {
		root.AnswerSave(request.Gen, true)
		root.ReportErr("connection not saved: " + err.Error())
		return nil
	}
	root.AnswerSave(request.Gen, false)
	s.SetList(stored)
	root.RebuildRows()
	change := RailChange{Focus: request.Next.Name}
	switch {
	case request.Op == SaveUpdate && request.Name != request.Next.Name:
		change = RailChange{Rename: request.Name, To: request.Next.Name, Focus: request.Next.Name}
	case request.Op == SaveRemove:
		change = RailChange{Forget: request.Name}
	}
	root.RailConnection(change)
	switch request.Op {
	case SaveRemove:
		root.ReportDone("removed " + request.Name + "; its sessions keep running on that host")
		return nil
	}
	root.ReportDone("connecting to " + request.Next.Name + " over SSH")
	return s.Poll()
}
