package install

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uilaunchhint "github.com/YoanWai/agent-manager/internal/ui/launchhint"
)

// Services is the work an install needs done off the update path.
type Services struct {
	Lifecycle *sessioncmd.Lifecycle
	Hooks     *hooks.Manager
	Driver    *tmux.Driver
	Tools     map[string]config.Tool
}

var (
	HomeDir        = os.UserHomeDir
	CheckInstalled = config.CheckInstalled
	SendText       = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
		return driver.SendTextResult(id, text)
	}
	SessionExists = func(driver *tmux.Driver, id string) (bool, error) {
		return driver.SessionExists(id)
	}
)

// RunStart launches the installer shell, types the script's line into it, and
// reports the pending install once that line was submitted.
func RunStart(services Services, request StartRequest) (StartResult, error) {
	result := StartResult{}
	if services.Lifecycle == nil || services.Hooks == nil || services.Driver == nil {
		return result, errors.New("install requires lifecycle, hook, and tmux services")
	}
	home, err := HomeDir()
	if err != nil {
		return result, err
	}
	tool, ok := services.Tools[request.ToolName]
	if !ok {
		return result, fmt.Errorf("shell tool %s is no longer configured", request.ToolName)
	}
	sess := store.Session{
		ID:     request.ID,
		Name:   "install-" + request.Binary,
		Tool:   request.ToolName,
		Cwd:    home,
		Group:  request.Group,
		Status: status.Starting,
	}
	statusFile := services.Hooks.InstallStatusFile(sess.ID)
	script, err := services.Hooks.WriteInstallScript(sess.ID, Script(request.Command, statusFile))
	if err != nil {
		return result, err
	}
	line, err := Line(script)
	if err != nil {
		removeFiles(statusFile, script)
		return result, err
	}
	launched, err := services.Lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:     sess,
		Tool:        tool,
		BaseCommand: tool.Command,
		Pane:        request.Pane,
	})
	if err != nil {
		removeFiles(statusFile, script)
		return result, err
	}
	result.Session = launched.Session
	if launched.LabelError != nil {
		removeFiles(statusFile, script)
		return result, launched.LabelError
	}
	pending := &Pending{
		SessionID:  sess.ID,
		Name:       sess.Name,
		Binary:     request.Binary,
		StatusFile: statusFile,
		Script:     script,
		Retry:      request.Retry,
		Images:     request.Images,
	}
	sendResult, sendErr := SendText(services.Driver, sess.ID, line)
	if sendErr == nil && sendResult.Phase != tmux.SendPhaseSubmitted {
		sendErr = errors.New("installer command was not confirmed submitted")
	}
	if sendErr != nil && !sendResult.PasteMayHaveStarted() {
		// The durable shell remains open like any other terminal. Only the
		// command files are retired because this install never began.
		removeFiles(statusFile, script)
		return result, sendErr
	}
	result.Pending = pending
	if sendErr != nil {
		return result, fmt.Errorf(
			"%s install may be running in %s; inspect that shell before retrying or quitting; Agent Manager will not send it again: %w",
			request.Binary, sess.Name, sendErr,
		)
	}
	return result, nil
}

func RunSettle(services Services, request SettleRequest) (SettleResult, error) {
	install := request.Pending
	result := SettleResult{SessionID: install.SessionID, State: uilaunchhint.Running}
	data, err := os.ReadFile(install.StatusFile)
	if errors.Is(err, fs.ErrNotExist) {
		exists, existsErr := SessionExists(services.Driver, install.SessionID)
		if existsErr != nil {
			return result, fmt.Errorf("check installer shell %s liveness: %w", install.Name, existsErr)
		}
		if exists {
			return result, nil
		}
		removeFiles(install.StatusFile, install.Script)
		DropImages(install.Images)
		result.State = uilaunchhint.ShellGone
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read installer status for %s: %w", install.Name, err)
	}
	// The shell creates the file before printf fills it. An empty file is
	// observed again on a later poll rather than read as a successful exit.
	code, state := uilaunchhint.ReadStatus(data)
	if state == uilaunchhint.Running {
		return result, nil
	}
	if state == uilaunchhint.Exited {
		removeFiles(install.StatusFile, install.Script)
		DropImages(install.Images)
		result.State = state
		result.Code = code
		return result, nil
	}
	if err := CheckInstalled(install.Binary); err != nil {
		var missing config.MissingToolError
		if errors.As(err, &missing) {
			removeFiles(install.StatusFile, install.Script)
			DropImages(install.Images)
			result.State = uilaunchhint.BinaryMissing
			return result, nil
		}
		return result, fmt.Errorf("%s installer finished, and looking for it failed: %w", install.Binary, err)
	}
	removeFiles(install.StatusFile, install.Script)
	result.State = uilaunchhint.Ready
	if install.Retry == nil {
		DropImages(install.Images)
	}
	return result, nil
}

// Starting names the install another accepted start is already running, which
// a second start must wait for. It reads a job's request, so the root hands it
// the requests on its lane.
func Starting(request uieffects.Request) (string, bool) {
	starting, ok := request.(StartRequest)
	return starting.Binary, ok
}
