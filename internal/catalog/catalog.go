// Package catalog asks each agent CLI, through the interface it offers
// programs, for the models, efforts and profiles a session can launch with.
// Every value comes from the CLI, so a model it ships next week shows up
// without a release.
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
	"time"

	"github.com/YoanWai/agent-manager/internal/catalog/value"
)

// Model is one model a CLI offers.
type Model = value.Model

// Profile is a named setup a CLI launches under, with the models it offers.
type Profile = value.Profile

// Catalog is what one CLI answered.
type Catalog = value.Catalog

// Match returns the model whose Key is key, else every model with that id.
func Match(models []Model, key string) []Model { return value.Match(models, key) }

// Default is the model the CLI starts on when none is given, if it says.
func Default(models []Model) (Model, bool) { return value.Default(models) }

// opencode reading its provider catalogs, the slowest, took about 20s.
const fetchTimeout = 45 * time.Second

var readers = map[string]func(ctx context.Context, command, dir string) (Catalog, error){
	"claude":   readClaude,
	"codex":    readCodex,
	"acp":      readACP,
	"pi":       readPi,
	"omp":      readOmp,
	"muse":     readMuse,
	"opencode": readOpencode,
	"hermes":   readHermes,
}

// Fetch starts command in dir and asks it over the interface named kind.
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

// process runs in a group of its own, so the servers and MCP children it
// starts leave with it.
type process struct {
	cmd      *exec.Cmd
	tree     tree
	stdin    io.WriteCloser
	lines    chan []byte
	quit     chan struct{}
	exited   chan struct{}
	waitErr  error
	stopOnce sync.Once
}

// running lets a caller exiting mid-answer stop CLIs its signals never reach.
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

// start reads a server's stderr too, since it may announce its address there.
func start(command, dir string, server bool, env ...string) (*process, error) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return nil, errors.New("empty catalog command")
	}
	return startArgs(fields[0], fields[1:], dir, server, env...)
}

// startArgs is start for a program path that may itself hold a space.
func startArgs(name string, args []string, dir string, server bool, env ...string) (*process, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	prepareTree(cmd)
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
	tree, err := adoptTree(cmd)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, err
	}
	proc := &process{cmd: cmd, tree: tree, stdin: stdin, lines: make(chan []byte, 64), quit: make(chan struct{}), exited: make(chan struct{})}
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

func (p *process) stop() {
	p.stopOnce.Do(func() {
		close(p.quit)
		_ = p.stdin.Close()
		p.tree.term()
		select {
		case <-p.exited:
		case <-time.After(2 * time.Second):
		}
		// Whether or not the server went, a child it left can still be
		// running, so the whole tree ends here.
		p.tree.close()
		running.Lock()
		delete(running.procs, p)
		running.Unlock()
	})
}

// secret keeps other local processes off a server this package starts.
func secret() string {
	return rand.Text()
}
