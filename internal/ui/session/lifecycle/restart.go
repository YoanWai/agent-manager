package lifecycle

import (
	"github.com/YoanWai/agent-manager/internal/config"
	uisession "github.com/YoanWai/agent-manager/internal/ui/session"
	"github.com/google/uuid"
)

// RestartLaunch is what a restart runs: the tool's plain launch command,
// exactly as a brand new session gets it, plus a fresh conversation id for the
// tools that take one. Tools that mint their own id instead get nothing to
// carry, and the poller captures what they wrote.
func RestartLaunch(tool config.Tool) (baseCommand, agentSessionID string) {
	if tool.SessionIDFlag == "" {
		return tool.Command, ""
	}
	agentSessionID = uuid.NewString()
	return tool.Command + " " + tool.SessionIDFlag + " " + agentSessionID, agentSessionID
}

// DegradedResume is the note a revive leaves for one tool: a session whose own
// conversation id was never captured falls back to the working directory's
// most recent conversation, which resumes the wrong one whenever sessions
// share a directory. Tools that resume by id or offer a picker never fall back.
func DegradedResume(tool config.Tool, agentSessionID, name string) string {
	if agentSessionID != "" || tool.ResumeByIDCommand == "" || tool.ResumePickerCommand != "" {
		return ""
	}
	return uisession.DegradedResumeText(name)
}
