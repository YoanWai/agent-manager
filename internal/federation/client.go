// Package federation joins host-owned registries without copying their state.
package federation

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	_ "modernc.org/sqlite"
)

type Ref struct{ Host, ID string }
type Row struct {
	Ref                                            Ref
	Name, Tool, Group, Directory, Status, ParentID string
	Running, Terminal                              bool
	Archived                                       bool
}
type HostSnapshot struct {
	Name       string
	Rows       []Row
	Groups     []string
	GroupPaths map[string]string
	Error      string
}
type Host struct {
	Name         string `json:"name"`
	SSH          string `json:"ssh"`
	Controller   string `json:"controller"`
	Binary       string `json:"binary"`
	ReviewBinary string `json:"review_binary,omitempty"`
}
type Client struct {
	dir   string
	hosts []Host
	run   func(context.Context, *exec.Cmd) ([]byte, error)
}

var safeTarget = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.@:-]*$`)
var safeID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func Load(configDir string) (*Client, error) {
	b, err := os.ReadFile(filepath.Join(configDir, "fleet.json"))
	if err != nil {
		return nil, err
	}
	var cfg struct {
		Hosts []Host `json:"hosts"`
	}
	if err = json.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("fleet.json: %w", err)
	}
	seen := map[string]bool{}
	localHosts := 0
	for i := range cfg.Hosts {
		h := &cfg.Hosts[i]
		if h.Name == "" || seen[h.Name] {
			return nil, fmt.Errorf("fleet host names must be nonempty and unique: %q", h.Name)
		}
		if strings.TrimSpace(h.Name) != h.Name || strings.Contains(h.Name, "/") || strings.Contains(h.Name, "::") || strings.IndexFunc(h.Name, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("invalid fleet host name %q: names cannot contain path or id separators, control characters, or surrounding whitespace", h.Name)
		}
		if h.SSH == "" {
			localHosts++
			if localHosts > 1 {
				return nil, fmt.Errorf("fleet.json may configure at most one local host")
			}
		}
		seen[h.Name] = true
		if h.SSH != "" && (!safeTarget.MatchString(h.SSH) || h.Controller == "") {
			return nil, fmt.Errorf("host %s needs a valid SSH destination and controller", h.Name)
		}
		if h.Controller != "" && !safeID.MatchString(h.Controller) {
			return nil, fmt.Errorf("host %s has invalid controller", h.Name)
		}
		if h.Binary == "" {
			h.Binary = "agent-manager"
		}
		if strings.ContainsAny(h.Binary, "\x00\r\n") {
			return nil, fmt.Errorf("invalid binary for %s", h.Name)
		}
		if strings.ContainsAny(h.ReviewBinary, "\x00\r\n") {
			return nil, fmt.Errorf("invalid review binary for %s", h.Name)
		}
	}
	if len(cfg.Hosts) == 0 {
		return nil, fmt.Errorf("fleet.json has no hosts")
	}
	return &Client{dir: configDir, hosts: cfg.Hosts, run: run}, nil
}
func run(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func (c *Client) host(name string) (Host, error) {
	for _, h := range c.hosts {
		if h.Name == name {
			return h, nil
		}
	}
	return Host{}, fmt.Errorf("unknown host %q", name)
}
func remote(ctx context.Context, h Host, tty bool, args ...string) *exec.Cmd {
	words := []string{"env", "AGENT_MANAGER_SESSION_ID=" + h.Controller, h.Binary}
	words = append(words, args...)
	for i := range words {
		words[i] = quote(words[i])
	}
	flags := []string{"-o", "BatchMode=yes", "-o", "ForwardAgent=no", "-o", "ClearAllForwardings=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=2", "-T"}
	if tty {
		flags[len(flags)-1] = "-t"
	}
	flags = append(flags, h.SSH, strings.Join(words, " "))
	return exec.CommandContext(ctx, "ssh", flags...)
}
func (c *Client) rpc(ctx context.Context, h Host, args ...string) ([]byte, error) {
	return c.run(ctx, remote(ctx, h, false, args...))
}
func (c *Client) Snapshot(ctx context.Context) []HostSnapshot {
	result := make([]HostSnapshot, len(c.hosts))
	var wg sync.WaitGroup
	for i, h := range c.hosts {
		wg.Add(1)
		go func(i int, h Host) {
			defer wg.Done()
			bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			rows, err := c.rows(bounded, h)
			result[i] = HostSnapshot{Name: h.Name, Rows: rows}
			if err == nil {
				result[i].Groups, result[i].GroupPaths, err = c.groups(bounded, h)
			}
			if err != nil {
				result[i].Error = err.Error()
				result[i].Rows = nil
			}
		}(i, h)
	}
	wg.Wait()
	return result
}
func (c *Client) groups(ctx context.Context, h Host) ([]string, map[string]string, error) {
	if h.SSH != "" {
		b, err := c.rpc(ctx, h, "groups", "--json")
		if err != nil {
			return nil, nil, err
		}
		var groups []sessioncmd.Group
		if err = json.Unmarshal(b, &groups); err != nil {
			return nil, nil, err
		}
		var names []string
		paths := map[string]string{}
		for _, g := range groups {
			if !g.Archived {
				names = append(names, g.Path)
				paths[g.Path] = g.Directory
			}
		}
		return names, paths, nil
	}
	path := filepath.Join(c.dir, "state.db")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil, nil
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(1000)")
	if err != nil {
		return nil, nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT name,path FROM groups WHERE archived=0 ORDER BY sort_order,name")
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var names []string
	paths := map[string]string{}
	for rows.Next() {
		var name, path string
		if err = rows.Scan(&name, &path); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		paths[name] = path
	}
	return names, paths, rows.Err()
}
func (c *Client) rows(ctx context.Context, h Host) ([]Row, error) {
	if h.SSH == "" {
		return c.localRows(ctx, h)
	}
	b, err := c.rpc(ctx, h, "sessions", "--json")
	if err != nil {
		return nil, err
	}
	var sessions []sessioncmd.Session
	if err = json.Unmarshal(b, &sessions); err != nil {
		return nil, fmt.Errorf("sessions JSON: %w", err)
	}
	result := []Row{}
	for _, s := range sessions {
		result = append(result, Row{Ref: Ref{h.Name, s.ID}, Name: s.Name, Tool: s.Tool, Group: s.Group, Directory: s.Directory, Status: s.Status, Running: s.Running, Archived: s.Archived})
	}
	b, err = c.rpc(ctx, h, "terminal", "list", "--json")
	if err != nil {
		return nil, err
	}
	var terminals []sessioncmd.Terminal
	if err = json.Unmarshal(b, &terminals); err != nil {
		return nil, fmt.Errorf("terminals JSON: %w", err)
	}
	for _, s := range terminals {
		result = append(result, Row{Ref: Ref{h.Name, s.ID}, Name: s.Name, Tool: "terminal", Group: s.Group, Directory: s.Directory, Status: s.Status, Running: s.Running, Terminal: true, ParentID: s.ParentID})
	}
	return result, nil
}
func (c *Client) localRows(ctx context.Context, h Host) ([]Row, error) {
	path := filepath.Join(c.dir, "state.db")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return []Row{}, nil
	} else if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=busy_timeout(1000)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT id,name,tool,group_name,cwd,status,parent_id FROM sessions WHERE archived=0 ORDER BY created_at,id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Row{}
	for rows.Next() {
		r := Row{Ref: Ref{Host: h.Name}}
		if err = rows.Scan(&r.Ref.ID, &r.Name, &r.Tool, &r.Group, &r.Directory, &r.Status, &r.ParentID); err != nil {
			return nil, err
		}
		r.Terminal = r.Tool == "terminal"
		result = append(result, r)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	// One read-only tmux query distinguishes an absent server from a failed probe.
	cmd := exec.CommandContext(ctx, "tmux", "-L", "agentmgr", "list-sessions", "-F", "#{session_name}")
	b, err := c.run(ctx, cmd)
	if err != nil && !strings.Contains(err.Error(), "no server running") && !strings.Contains(err.Error(), "error connecting to") {
		return nil, err
	}
	live := map[string]bool{}
	for _, name := range strings.Fields(string(b)) {
		live[strings.TrimPrefix(name, "am_")] = true
	}
	for i := range result {
		result[i].Running = live[result[i].Ref.ID]
		if !result[i].Running {
			result[i].Status = "dead"
		}
	}
	return result, nil
}
func (c *Client) Attach(ref Ref) *exec.Cmd {
	h, err := c.host(ref.Host)
	if err != nil || !safeID.MatchString(ref.ID) {
		return nil
	}
	if h.SSH != "" {
		h.Binary = "tmux"
		return remote(context.Background(), h, true, "-u", "-L", "agentmgr", "attach-session", "-t", "am_"+ref.ID)
	}
	cmd := exec.Command("tmux", "-u", "-L", "agentmgr", "attach-session", "-t", "am_"+ref.ID)
	cmd.Env = withoutTMUX()
	return cmd
}
func withoutTMUX() []string {
	var result []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "TMUX=") {
			result = append(result, value)
		}
	}
	return result
}
func (c *Client) Manager(host string) *exec.Cmd {
	h, err := c.host(host)
	if err != nil {
		return nil
	}
	if h.SSH != "" {
		return remote(context.Background(), h, true)
	}
	return exec.Command(h.Binary)
}
func (c *Client) Send(ctx context.Context, ref Ref, text string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h, err := c.host(ref.Host)
	if err != nil {
		return "", err
	}
	rows, err := c.rows(ctx, h)
	if err != nil {
		return "", err
	}
	found := false
	for _, r := range rows {
		if r.Ref == ref {
			found = true
			if r.Terminal {
				return "", fmt.Errorf("cannot send an agent message to a terminal; attach to it instead")
			}
		}
	}
	if !found {
		return "", fmt.Errorf("session no longer exists")
	}
	if h.SSH != "" {
		if h.Controller == ref.ID {
			return "", fmt.Errorf("the configured controller cannot message itself; attach to it or choose another controller")
		}
		b, e := c.rpc(ctx, h, "send", ref.ID, text, "--json")
		return string(b), e
	}
	caller := h.Controller
	if caller == "" {
		for _, r := range rows {
			if !r.Terminal && r.Ref.ID != ref.ID {
				caller = r.Ref.ID
				break
			}
		}
	}
	if caller == "" {
		return "", fmt.Errorf("configure a local controller session before sending messages")
	}
	result, err := sessioncmd.NewSessions(c.dir, sessioncmd.CLIVocabulary()).Send(caller, ref.ID, text)
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(result)
	return string(b), err
}

// Capture preserves terminal styling for the human UI; Read remains plain text.
func (c *Client) Capture(ctx context.Context, ref Ref) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h, err := c.host(ref.Host)
	if err != nil {
		return "", err
	}
	if !safeID.MatchString(ref.ID) {
		return "", fmt.Errorf("invalid session id")
	}
	args := []string{"-L", "agentmgr", "capture-pane", "-p", "-e", "-t", paneTarget(ref.ID)}
	var b []byte
	if h.SSH == "" {
		b, err = c.run(ctx, exec.CommandContext(ctx, "tmux", args...))
	} else {
		h.Binary = "tmux"
		b, err = c.rpc(ctx, h, args...)
	}
	return string(b), err
}

func (c *Client) Read(ctx context.Context, ref Ref, terminal bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	h, err := c.host(ref.Host)
	if err != nil {
		return "", err
	}
	if !safeID.MatchString(ref.ID) {
		return "", fmt.Errorf("invalid session id")
	}
	if h.SSH == "" {
		b, e := c.run(ctx, exec.CommandContext(ctx, "tmux", "-L", "agentmgr", "capture-pane", "-p", "-t", paneTarget(ref.ID)))
		return string(b), e
	}
	args := []string{"read", ref.ID, "--json"}
	if terminal {
		args = append([]string{"terminal"}, args...)
	}
	b, err := c.rpc(ctx, h, args...)
	if err != nil {
		return "", err
	}
	var screen struct {
		Output string `json:"output"`
	}
	err = json.Unmarshal(b, &screen)
	return screen.Output, err
}
