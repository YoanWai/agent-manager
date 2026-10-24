package source

import (
	"errors"

	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	tea "github.com/charmbracelet/bubbletea"
)

// Preferences are the stored Review preferences that open a session's Review.
type Preferences interface {
	ReviewScope(string) (string, error)
	ReviewRepo(string) (string, error)
}

// PreferencesMsg carries the preferences read for one opened review. The
// session and request it carries are what the completion is fenced against.
type PreferencesMsg struct {
	Session   store.Session
	Request   uireview.LoadRequest
	Scope     git.Scope
	Preferred string
	Err       error
}

// PreferencesCmd reads the stored scope and repo a review opens on, with the
// request the review would use if the read kept its defaults.
func PreferencesCmd(reader Preferences, session store.Session, request uireview.LoadRequest, preferred string, picked bool) tea.Cmd {
	return func() tea.Msg {
		result := PreferencesMsg{Session: session, Request: request, Scope: git.ScopeUncommitted, Preferred: preferred}
		if reader == nil {
			return result
		}
		scope, err := reader.ReviewScope(session.ID)
		result.Err = err
		if err == nil {
			result.Scope = uireview.StoredScope(scope)
		}
		if !picked {
			repo, err := reader.ReviewRepo(session.ID)
			result.Preferred = repo
			result.Err = errors.Join(result.Err, err)
		}
		return result
	}
}
