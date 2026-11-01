package apply

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/agentsession"
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/google/uuid"
)

// ForkSessionFile resolves a source conversation's on-disk session file for
// tools whose fork loads a file instead of an id (gemini), keyed by the
// tool's session_store. It is a variable so tests substitute a fixture
// without touching the real store.
var ForkSessionFile = agentsession.SessionFile

// ForkRequest freezes a fork dialog submission: the source's manager id,
// conversation and worktree ownership, the typed name, and the dialog
// generation it was submitted from. The worker re-reads the source by
// manager id to validate liveness; a source restarted or moved while the
// fork was queued is refused, and the fork targets the captured identity,
// not the replacement. A conversation the running agent reported switching
// to is followed. A retry keeps the child id and pane the first attempt
// captured, so a second submit does not fork twice.
type ForkRequest struct {
	ChildID     string
	Source      store.Session
	Name        string
	Gen         int
	KnownForkID string
	Pane        sessioncmd.PaneSize
}

// Copy owns the request's mutable source fields, so a later edit of the row
// the dialog captured cannot retarget the queued work.
func (f ForkRequest) Copy() ForkRequest {
	f.Source.PendingInputs = slices.Clone(f.Source.PendingInputs)
	f.Source.RelaunchSnapshot = maps.Clone(f.Source.RelaunchSnapshot)
	return f
}

func (f *ForkRequest) MintChildID() { f.ChildID = newID() }

// ForkOutcome is the durable result of a fork. Launched is set once the
// child session is committed, even when the launch then fails at labeling,
// so the row applies before the error reports. RecordedForkID is the fork
// the source's own store recorded once the fork keys landed; it survives a
// failed child launch so a retry reuses it instead of forking the source a
// second time.
type ForkOutcome struct {
	ChildID        string
	Launched       store.Session
	RecordedForkID string
}

// inSourceWait bounds how long the tool may take to record the fork once the
// keys land.
const inSourceWait = 10 * time.Second

func Fork(s Sources, preflight Preflight, request ForkRequest) (ForkOutcome, error) {
	result := ForkOutcome{ChildID: request.ChildID}
	if result.ChildID == "" {
		result.ChildID = newID()
	}
	source, err := s.Store.Get(request.Source.ID)
	if err != nil {
		return result, err
	}
	if err := s.Lifecycle.ValidatePlacement(source); err != nil {
		return result, err
	}
	// The re-read validates liveness, not identity: the fork targets the
	// source the dialog confirmed. A restart while queued mints a new
	// launch, and forking it would open the replacement. A conversation
	// the agent switched to and reported keeps its launch, and the fork
	// branches from that one.
	switched := source.AgentSessionID != request.Source.AgentSessionID
	if switched && source.AgentSessionID != "" && source.AgentLaunchedAt.Equal(request.Source.AgentLaunchedAt) {
		request.Source.AgentSessionID = source.AgentSessionID
		switched = false
	}
	if source.Tool != request.Source.Tool ||
		source.Group != request.Source.Group ||
		source.ParentID != request.Source.ParentID ||
		switched ||
		source.Cwd != request.Source.Cwd ||
		source.WorktreeRepo != request.Source.WorktreeRepo ||
		source.WorktreeBranch != request.Source.WorktreeBranch {
		return result, fmt.Errorf("%s changed while the fork was in the queue; reopen the fork dialog", source.Name)
	}
	tool, err := ForkSource(s.Config, request.Source)
	if err != nil {
		return result, err
	}
	if !preflight.ResolveDir(request.Source.Cwd) {
		return result, fmt.Errorf("working directory no longer exists: %s", request.Source.Cwd)
	}
	agentID := ""
	var baseCommand string
	chosen := tool.WithChoice(source.Choice)
	forkCommand := chosen.ForkCommand
	switch {
	case tool.ForkKeys != "":
		if request.KnownForkID != "" {
			// A retry after a recorded fork: the keys already landed, and
			// typing them again would make the source fork a second time.
			known, ok := agentsession.Forks(tool.SessionStore, request.Source.AgentSessionID)
			if !ok {
				return result, fmt.Errorf("cannot read the forks %s recorded of %s", source.Tool, source.Name)
			}
			if !known[request.KnownForkID] {
				return result, fmt.Errorf("the fork %s %s recorded is gone; fork again", request.KnownForkID, source.Name)
			}
			agentID = request.KnownForkID
			baseCommand = ExpandCommand(forkCommand, source.AgentSessionID, agentID, request.Name, "")
		} else {
			earlier, ok := agentsession.Forks(tool.SessionStore, source.AgentSessionID)
			if !ok {
				return result, fmt.Errorf("cannot read the forks %s recorded of %s", source.Tool, source.Name)
			}
			since := time.Now()
			if err := s.Runner.TypeForkKeys(source, tool.ForkKeys); err != nil {
				return result, fmt.Errorf("fork key delivery outcome is uncertain; inspect the source before trying again: %w", err)
			}
			for deadline := since.Add(inSourceWait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
				if id, ok := agentsession.ForkedFrom(tool.SessionStore, source.AgentSessionID, since, earlier); ok {
					agentID = id
					result.RecordedForkID = id
					baseCommand = ExpandCommand(forkCommand, source.AgentSessionID, id, request.Name, "")
					break
				}
			}
			if agentID == "" {
				return result, fmt.Errorf("%s did not record a fork of %s; the outcome is uncertain, inspect the source before trying again", source.Tool, source.Name)
			}
		}
	default:
		switch {
		case !strings.Contains(tool.ForkCommand, "{new_id}"):
		case !agentsession.MintsForkID(tool.SessionStore):
			agentID = uuid.NewString()
		case request.KnownForkID != "":
			// A retry after a recorded fork: the store already made the
			// copy, and asking again would fork the source a second time.
			agentID = request.KnownForkID
		default:
			id, err := agentsession.Fork(tool.SessionStore, request.Source.Cwd, source.AgentSessionID)
			if err != nil {
				return result, err
			}
			agentID = id
			result.RecordedForkID = id
		}
		sessionFile := ""
		if strings.Contains(tool.ForkCommand, "{session_file}") {
			resolved, err := ForkSessionFile(tool.SessionStore, source.AgentSessionID)
			if err != nil {
				return result, err
			}
			sessionFile = resolved
		}
		baseCommand = ExpandCommand(forkCommand, source.AgentSessionID, agentID, request.Name, sessionFile)
	}
	forked := store.Session{
		ID:             result.ChildID,
		Name:           request.Name,
		Tool:           request.Source.Tool,
		Cwd:            request.Source.Cwd,
		Group:          request.Source.Group,
		Status:         status.Starting,
		AgentSessionID: agentID,
		WorktreeRepo:   request.Source.WorktreeRepo,
		WorktreeBranch: request.Source.WorktreeBranch,
		Choice:         request.Source.Choice,
		Environment:    request.Source.Environment,
	}
	launched, err := s.Lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:     forked,
		Tool:        chosen,
		BaseCommand: baseCommand,
		Pane:        request.Pane,
	})
	if err != nil {
		return result, err
	}
	result.Launched = launched.Session
	return result, launched.LabelError
}

// ForkSource is the CLI a source forks with. A tool pane has no
// conversation to fork, so it is refused before any CLI field is named.
func ForkSource(cfg config.Config, source store.Session) (config.CLI, error) {
	if cfg.IsTool(source.Tool) {
		return config.CLI{}, fmt.Errorf("%s is a shell, not an agent - there is no conversation to fork", source.Name)
	}
	tool, ok := cfg.CLIs[source.Tool]
	if !ok {
		return config.CLI{}, fmt.Errorf("CLI %s is no longer configured", source.Tool)
	}
	return tool, ValidateForkSource(source.Tool, tool, source)
}

// ValidateForkSource refuses a source its CLI cannot fork, naming the piece
// the configuration is missing.
func ValidateForkSource(toolName string, tool config.CLI, source store.Session) error {
	if tool.ForkCommand == "" {
		return fmt.Errorf("CLI %s has no fork_command", toolName)
	}
	usesSessionFile := strings.Contains(tool.ForkCommand, "{session_file}")
	mintsFork := strings.Contains(tool.ForkCommand, "{new_id}") && agentsession.MintsForkID(tool.SessionStore)
	if !strings.Contains(tool.ForkCommand, "{id}") && !usesSessionFile && tool.ForkKeys == "" && !mintsFork {
		return fmt.Errorf("CLI %s fork_command must reference the source via {id} or {session_file}", toolName)
	}
	if usesSessionFile && !agentsession.SupportsSessionFile(tool.SessionStore) {
		return fmt.Errorf("CLI %s fork_command uses {session_file}, which needs session_store = \"gemini\"", toolName)
	}
	if source.AgentSessionID == "" {
		return fmt.Errorf("%s has no captured conversation id", source.Name)
	}
	return nil
}

// ExpandCommand fills a tool's fork_command placeholders, quoting each value
// for the pane's shell.
func ExpandCommand(template, sourceID, newID, name, sessionFile string) string {
	return strings.NewReplacer(
		"{id}", tmux.ShellQuote(sourceID),
		"{new_id}", tmux.ShellQuote(newID),
		"{name}", tmux.ShellQuote(name),
		"{session_file}", tmux.ShellQuote(sessionFile),
	).Replace(template)
}
