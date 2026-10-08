package ui

import (
	"errors"
	"fmt"
	"slices"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type quickSendOutcome uint8

const (
	quickSendRefused quickSendOutcome = iota
	quickSendUncertain
	quickSendConfirmed
)

// quickSendRequest freezes the selected pane and the prompt that Enter
// accepted. The worker may finish after the cursor or composer has moved.
type quickSendRequest struct {
	session        store.Session
	composerGen    int
	draft          string
	text           string
	closeAfterSend bool
	images         []imageAttachment
}

func (quickSendRequest) effectRequest() {}

type quickSendResult struct {
	outcome     quickSendOutcome
	sendErr     error
	metadataErr error
}

func (quickSendResult) effectResult() {}

func (m *Model) dispatchQuickSend(request quickSendRequest) bool {
	jobs := append([]*effectJob{m.effects.active}, m.effects.pending...)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		pending, ok := job.request.(quickSendRequest)
		if ok && pending.composerGen == request.composerGen {
			m.reportErr("this prompt is already being sent")
			return false
		}
	}
	request.images = slices.Clone(request.images)
	m.quick.attachments = nil
	m.enqueueEffect(request, 0, false)
	return true
}

func (s effectServices) runQuickSend(request quickSendRequest) (effectResult, error) {
	result := quickSendResult{outcome: quickSendRefused}
	current, err := s.store.Get(request.session.ID)
	if err != nil {
		return result, err
	}
	if !sameQuickSendTarget(current, request.session) {
		return result, errors.New("quick-send target changed its identity, tool, launch, or socket before the accepted prompt ran")
	}
	if s.cfg.Tools[current.Tool].Shell {
		return result, errors.New(shellPromptHint(current.Name))
	}
	exists, err := s.quickSessionExists(s.driver, current.ID)
	if err != nil {
		return result, fmt.Errorf("check quick-send target %s: %w", current.Name, err)
	}
	if !exists {
		return result, errors.New(deadSessionHint)
	}
	sent, sendErr := s.quickSendText(s.driver, current.ID, request.text)
	if sendErr != nil {
		result.sendErr = sendErr
		if sent.PasteMayHaveStarted() {
			result.outcome = quickSendUncertain
		}
		return result, nil
	}
	result.outcome = quickSendConfirmed
	result.metadataErr = s.store.RecordQuickSendForLaunch(request.session, request.text)
	return result, nil
}

func sameQuickSendTarget(current, captured store.Session) bool {
	return current.ID == captured.ID &&
		current.CreatedAt.Equal(captured.CreatedAt) &&
		current.Tool == captured.Tool &&
		current.LaunchTime().Equal(captured.LaunchTime()) &&
		current.TmuxSocket == captured.TmuxSocket
}

func (m *Model) applyQuickSend(request quickSendRequest, result quickSendResult, err error) tea.Cmd {
	if err != nil || result.outcome == quickSendRefused {
		cleanup := m.returnQuickSendImages(request)
		if err != nil {
			m.reportErr(err.Error())
		} else {
			m.reportErr(fmt.Sprintf("prompt was not sent to %s: %v", request.session.Name, result.sendErr))
		}
		return cleanup
	}

	m.quick.clearAccepted(request)
	// Once paste may have started, the pane may still need image paths from
	// the accepted text. They remain owned by that delivery, never by a retry.
	if result.outcome == quickSendUncertain {
		m.reportErr(fmt.Sprintf("prompt may have reached %s and was not sent again: %v", request.session.Name, result.sendErr))
		return nil
	}
	if result.metadataErr != nil {
		m.reportErr("prompt sent, but recording it for the row failed: " + result.metadataErr.Error())
		return nil
	}

	for i := range m.workspace.sessions {
		if sameQuickSendTarget(m.workspace.sessions[i], request.session) {
			m.workspace.sessions[i].Acked = false
			m.workspace.sessions[i].LastPrompt = request.text
		}
	}
	m.clearErr()
	return nil
}

// clearAccepted empties the bar a confirmed send came from, unless its
// prompt has changed since.
func (q *quickBar) clearAccepted(request quickSendRequest) {
	if !q.active || q.gen != request.composerGen || q.input.Value() != request.draft || len(q.attachments) != 0 {
		return
	}
	q.input.SetValue("")
	if request.closeAfterSend {
		q.active = false
	}
}

func (m *Model) returnQuickSendImages(request quickSendRequest) tea.Cmd {
	if m.quick.active && m.quick.gen == request.composerGen {
		m.quick.attachments = append(request.images, m.quick.attachments...)
		return nil
	}
	if len(request.images) == 0 {
		return nil
	}
	images := slices.Clone(request.images)
	return func() tea.Msg {
		dropImages(images)
		return nil
	}
}
