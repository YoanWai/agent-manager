// Package catalog asks an agent CLI, through the machine interface it offers
// programs, which models, reasoning efforts and profiles a session can
// launch with. Nothing here names a model or a level: every value comes from
// the CLI, so a model it ships next week shows up without a release.
package catalog

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Model is one model a CLI offers, with what its launch flags need.
type Model struct {
	ID string `json:"id"`
	// Provider routes ID for a CLI that picks models per provider.
	Provider string `json:"provider,omitempty"`
	Label    string `json:"label,omitempty"`
	// Efforts are the reasoning levels the model takes, in the CLI's order.
	Efforts       []string `json:"efforts,omitempty"`
	DefaultEffort string   `json:"default_effort,omitempty"`
	// EffortTyped marks a model that reasons while the CLI lists no levels
	// for it, so the level is typed rather than picked.
	EffortTyped bool `json:"effort_typed,omitempty"`
	// Default is the model the CLI starts on when no model is given.
	Default bool `json:"default,omitempty"`
}

// Key names the model the way a user writes it: the id, prefixed with its
// provider where the CLI routes by provider.
func (m Model) Key() string {
	if m.Provider == "" {
		return m.ID
	}
	return m.Provider + ":" + m.ID
}

// Profile is a named setup a CLI can launch under, with the models it offers.
type Profile struct {
	Name   string  `json:"name"`
	Detail string  `json:"detail,omitempty"`
	Models []Model `json:"models,omitempty"`
}

// Catalog is what one CLI answered.
type Catalog struct {
	Models   []Model   `json:"models"`
	Profiles []Profile `json:"profiles,omitempty"`
}

// ModelsFor is the model list a launch under profile chooses from; the
// empty profile is the CLI's own.
func (c Catalog) ModelsFor(profile string) []Model {
	for _, p := range c.Profiles {
		if p.Name == profile {
			return p.Models
		}
	}
	return c.Models
}

// Match returns the models key names: the one whose Key it is, else every
// model with that id, which is more than one where providers share an id.
func Match(models []Model, key string) []Model {
	var byID []Model
	for _, model := range models {
		if model.Key() == key {
			return []Model{model}
		}
		if model.ID == key {
			byID = append(byID, model)
		}
	}
	return byID
}

// Default is the model the CLI starts on when none is given, if it says.
func Default(models []Model) (Model, bool) {
	for _, model := range models {
		if model.Default {
			return model, true
		}
	}
	return Model{}, false
}

// fetchTimeout bounds one answer; opencode reading its provider catalogs is
// the slowest measured, at about twenty seconds.
const fetchTimeout = 45 * time.Second

var readers = map[string]func(ctx context.Context, command, dir string) (Catalog, error){
	"claude":   readClaude,
	"codex":    readCodex,
	"acp":      readACP,
	"pi":       readPi,
	"muse":     readMuse,
	"opencode": readOpencode,
	"hermes":   readHermes,
}

// Fetch starts command in dir and asks it over the interface kind names.
func Fetch(ctx context.Context, kind, command, dir string) (Catalog, error) {
	read, ok := readers[kind]
	if !ok {
		return Catalog{}, fmt.Errorf("no catalog reader named %q", kind)
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	cat, err := read(ctx, command, dir)
	if errors.Is(err, context.DeadlineExceeded) {
		return Catalog{}, fmt.Errorf("no answer within %s", fetchTimeout)
	}
	return cat, err
}

// process is a CLI serving one of the interfaces, in a process group of its
// own so the servers and MCP children it starts leave with it.
type process struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	lines    chan []byte
	quit     chan struct{}
	exited   chan struct{}
	waitErr  error
	stopOnce sync.Once
}

// running is every CLI still answering. Its own process group keeps a CLI
// out of the terminal's signals, so a caller exiting mid-answer stops them.
var running = struct {
	sync.Mutex
	procs map[*process]struct{}
}{procs: map[*process]struct{}{}}

// StopAll ends every CLI still answering.
func StopAll() {
	running.Lock()
	procs := make([]*process, 0, len(running.procs))
	for proc := range running.procs {
		procs = append(procs, proc)
	}
	running.Unlock()
	for _, proc := range procs {
		proc.stop()
	}
}

// start runs command in dir with env added to the manager's own, reading
// its output a line at a time: stdout, and for a server stderr too, since a
// server may announce its address on either.
func start(command, dir string, server bool, env ...string) (*process, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("empty catalog command")
	}
	cmd := exec.Command(fields[0], fields[1:]...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if server {
		cmd.Stderr = cmd.Stdout
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	proc := &process{cmd: cmd, stdin: stdin, lines: make(chan []byte, 64), quit: make(chan struct{}), exited: make(chan struct{})}
	running.Lock()
	running.procs[proc] = struct{}{}
	running.Unlock()
	go proc.readLines(bufio.NewReaderSize(stdout, 1<<20))
	return proc, nil
}

func (p *process) readLines(out *bufio.Reader) {
	for {
		line, err := out.ReadBytes('\n')
		if len(line) > 0 {
			select {
			case p.lines <- line:
			case <-p.quit:
			}
		}
		if err != nil {
			p.waitErr = p.cmd.Wait()
			close(p.lines)
			close(p.exited)
			return
		}
	}
}

// next returns the process's next output line.
func (p *process) next(ctx context.Context) ([]byte, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case line, ok := <-p.lines:
		if ok {
			return line, nil
		}
		if p.waitErr != nil {
			return nil, fmt.Errorf("exited before answering: %w", p.waitErr)
		}
		return nil, errors.New("exited before answering")
	}
}

func (p *process) send(line []byte) error {
	_, err := p.stdin.Write(append(line, '\n'))
	return err
}

// stop ends the whole group, asking first.
func (p *process) stop() {
	p.stopOnce.Do(func() {
		close(p.quit)
		_ = p.stdin.Close()
		group := -p.cmd.Process.Pid
		_ = syscall.Kill(group, syscall.SIGTERM)
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
			_ = syscall.Kill(group, syscall.SIGKILL)
		}
		running.Lock()
		delete(running.procs, p)
		running.Unlock()
	})
}

// secret is a one-use credential for a server this package starts, so no
// other local process can talk to it while it runs.
func secret() string {
	return rand.Text()
}
