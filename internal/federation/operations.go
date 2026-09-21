package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/google/uuid"
)

func (r Ref) String() string { return r.Host + "::" + r.ID }
func ParseRef(value string) (Ref, bool) {
	host, id, ok := strings.Cut(value, "::")
	return Ref{host, id}, ok
}
func (c *Client) Hosts() []Host             { return append([]Host(nil), c.hosts...) }
func (c *Client) IsRemote(host string) bool { h, err := c.host(host); return err == nil && h.SSH != "" }

// Command executes the remote host's normal CLI, retaining its own permission
// and ownership checks. It never invokes the remote MCP server recursively.
func (c *Client) Command(ctx context.Context, host string, args ...string) ([]byte, error) {
	h, err := c.host(host)
	if err != nil {
		return nil, err
	}
	if h.SSH == "" {
		return nil, fmt.Errorf("%s is local; use the local session service", host)
	}
	bound := 60 * time.Second
	if len(args) > 0 && args[0] == "wait" {
		for i, arg := range args {
			if arg == "--timeout" && i+1 < len(args) {
				if requested, e := time.ParseDuration(args[i+1]); e == nil && requested > 0 && requested <= 5*time.Minute {
					bound = requested + 10*time.Second
				}
			}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, bound)
	defer cancel()
	return c.rpc(ctx, h, args...)
}
func decodeCommand[T any](c *Client, ctx context.Context, host string, args ...string) (T, error) {
	var result T
	b, err := c.Command(ctx, host, args...)
	if err == nil {
		err = json.Unmarshal(b, &result)
	}
	return result, err
}
func (c *Client) Create(ctx context.Context, host string, o sessioncmd.CreateSessionOptions) (sessioncmd.Session, error) {
	args := []string{"spawn", "--json"}
	for _, pair := range [][2]string{{"--name", o.Name}, {"--tool", o.Tool}, {"--directory", o.Directory}, {"--prompt", o.Prompt}} {
		if pair[1] != "" {
			args = append(args, pair[0], pair[1])
		}
	}
	if o.Group != nil {
		args = append(args, "--group", *o.Group)
	}
	if o.Worktree != nil {
		args = append(args, fmt.Sprintf("--worktree=%t", *o.Worktree))
	}
	return decodeCommand[sessioncmd.Session](c, ctx, host, args...)
}
func (c *Client) Lifecycle(ctx context.Context, r Ref, action string, restore bool) (sessioncmd.Session, error) {
	if action != "kill" && action != "revive" && action != "archive" {
		return sessioncmd.Session{}, fmt.Errorf("unsupported lifecycle action %q", action)
	}
	args := []string{action, r.ID, "--json"}
	if action == "archive" && restore {
		args = append(args, "--restore")
	}
	return decodeCommand[sessioncmd.Session](c, ctx, r.Host, args...)
}
func (c *Client) TerminalCreate(ctx context.Context, host string, o sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	args := []string{"terminal", "create", "--json"}
	if o.Group != nil {
		args = append(args, "--group", *o.Group)
	}
	if o.Directory != "" {
		args = append(args, "--directory", o.Directory)
	}
	if o.Nest != nil {
		args = append(args, fmt.Sprintf("--nest=%t", *o.Nest))
	}
	return decodeCommand[sessioncmd.Terminal](c, ctx, host, args...)
}
func (c *Client) TerminalSend(ctx context.Context, r Ref, command string, keys []string) (sessioncmd.TerminalInput, error) {
	args := []string{"terminal", "send", r.ID, "--json"}
	if command != "" {
		args = append(args, "--command", command)
	}
	if len(keys) > 0 {
		args = append(args, "--keys", strings.Join(keys, ","))
	}
	return decodeCommand[sessioncmd.TerminalInput](c, ctx, r.Host, args...)
}
func (c *Client) TerminalClose(ctx context.Context, r Ref) error {
	_, err := c.Command(ctx, r.Host, "terminal", "close", r.ID)
	return err
}

type SpawnOptions struct {
	Tool, Name, Directory, Group, Prompt string
	Worktree                             bool
}

func (c *Client) Spawn(ctx context.Context, host string, o SpawnOptions) (string, error) {
	s, err := c.Create(ctx, host, sessioncmd.CreateSessionOptions{Tool: o.Tool, Name: o.Name, Directory: o.Directory, Group: &o.Group, Prompt: o.Prompt, Worktree: &o.Worktree})
	return s.ID, err
}
func (c *Client) Action(ctx context.Context, r Ref, action string) (string, error) {
	restore := action == "restore"
	if restore {
		action = "archive"
	}
	s, err := c.Lifecycle(ctx, r, action, restore)
	return s.ID, err
}

// TerminalCreateFor keeps the native terminal ownership rule: selecting an
// agent opens under that agent, selecting a terminal opens beside it.
func (c *Client) TerminalCreateFor(ctx context.Context, host, callerID string, o sessioncmd.CreateTerminalOptions) (sessioncmd.Terminal, error) {
	h, err := c.host(host)
	if err != nil {
		return sessioncmd.Terminal{}, err
	}
	if h.SSH == "" {
		return sessioncmd.Terminal{}, fmt.Errorf("use local terminal service")
	}
	if callerID != "" {
		if !safeID.MatchString(callerID) {
			return sessioncmd.Terminal{}, fmt.Errorf("invalid caller id")
		}
		h.Controller = callerID
	}
	args := []string{"terminal", "create", "--json"}
	if o.Group != nil {
		args = append(args, "--group", *o.Group)
	}
	if o.Directory != "" {
		args = append(args, "--directory", o.Directory)
	}
	if o.Nest != nil {
		args = append(args, fmt.Sprintf("--nest=%t", *o.Nest))
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	b, err := c.rpc(ctx, h, args...)
	var result sessioncmd.Terminal
	if err == nil {
		err = json.Unmarshal(b, &result)
	}
	return result, err
}
func (c *Client) SendKeys(ctx context.Context, r Ref, keys []string) error {
	h, err := c.host(r.Host)
	if err != nil {
		return err
	}
	if h.SSH == "" || !safeID.MatchString(r.ID) {
		return fmt.Errorf("invalid remote pane")
	}
	h.Binary = "tmux"
	args := []string{"-L", "agentmgr", "send-keys", "-t", paneTarget(r.ID), "--"}
	args = append(args, keys...)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = c.rpc(ctx, h, args...)
	return err
}

// SendText is literal terminal input from the focused human UI, never a queued
// agent instruction. A paste uses tmux's bracketed-paste support.
func (c *Client) SendText(ctx context.Context, r Ref, text string, paste bool) error {
	h, err := c.host(r.Host)
	if err != nil {
		return err
	}
	if h.SSH == "" || !safeID.MatchString(r.ID) {
		return fmt.Errorf("invalid remote pane")
	}
	h.Binary = "tmux"
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if !paste && len(text) < 1024 {
		_, err = c.rpc(ctx, h, "-L", "agentmgr", "send-keys", "-l", "-t", paneTarget(r.ID), "--", text)
		return err
	}
	buffer := "am-fleet-" + uuid.NewString()
	cmd := remote(ctx, h, false, "-L", "agentmgr", "load-buffer", "-b", buffer, "-")
	cmd.Stdin = strings.NewReader(text)
	if _, err = c.run(ctx, cmd); err != nil {
		return err
	}
	_, err = c.rpc(ctx, h, "-L", "agentmgr", "paste-buffer", "-d", "-p", "-b", buffer, "-t", paneTarget(r.ID))
	if err != nil {
		// The input context may have expired after loading the buffer. Give
		// cleanup its own short lifetime so clipboard content is not retained.
		cleanup, cancelCleanup := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelCleanup()
		if _, cleanupErr := c.rpc(cleanup, h, "-L", "agentmgr", "delete-buffer", "-b", buffer); cleanupErr != nil {
			return fmt.Errorf("paste failed: %w; remote buffer cleanup also failed: %v", err, cleanupErr)
		}
	}
	return err
}
