package source

import (
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// Message is the root handler a routed message asks for. The Review runtime
// reads its own messages here; the root hands each one to the code that
// touches the screen, the lane or the store.
type Message uint8

const (
	MessageNone Message = iota
	MessagePickerLoaded
	MessagePicked
	MessagePreferences
	MessageLoad
	MessageFile
	MessageFiles
	MessageHighlight
	MessageProbe
	MessageStatus
	MessageSave
	MessageHandle
	MessageSend
)

func Route(msg tea.Msg) (Message, bool) {
	switch msg.(type) {
	case PickerLoadedMsg:
		return MessagePickerLoaded, true
	case uireview.PickedMsg:
		return MessagePicked, true
	case PreferencesMsg:
		return MessagePreferences, true
	case LoadMsg:
		return MessageLoad, true
	case uireview.FileResult:
		return MessageFile, true
	case FilesResult:
		return MessageFiles, true
	case uireview.HighlightResult:
		return MessageHighlight, true
	case uireview.ProbeResult:
		return MessageProbe, true
	case uireview.StatusResult:
		return MessageStatus, true
	case uireview.SaveResult:
		return MessageSave, true
	case uireview.HandleCommentResult:
		return MessageHandle, true
	case uireview.SendResult:
		return MessageSend, true
	}
	return MessageNone, false
}
