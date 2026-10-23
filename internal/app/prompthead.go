package app

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
)

// ErrNotAtPromptHead is a prompt-head answer of no: ← belongs to the agent.
var ErrNotAtPromptHead = errors.New("the caret is not at the head of the prompt")

// PromptHead answers whether the agent in the named tmux session has its
// caret at the head of its prompt, the same question focus asks of ←.
// Anything that cannot be read answers no, so ← reaches the agent.
func PromptHead(profileDir string, driver *tmux.Driver, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: agent-manager " + sessioncmd.PromptHeadCommand + " <tmux session name>")
	}
	id, managed := strings.CutPrefix(args[0], "am_")
	if !managed || id == "" {
		return ErrNotAtPromptHead
	}
	st, err := store.Open(filepath.Join(profileDir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	if on, err := st.ArrowStep(); err != nil || !on {
		return errors.Join(err, ErrNotAtPromptHead)
	}
	sess, err := st.Get(id)
	if err != nil {
		return errors.Join(err, ErrNotAtPromptHead)
	}
	cfg, err := config.Default()
	if err != nil {
		return err
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		return err
	}
	cursor, err := paneCursor(driver, id)
	if err != nil {
		return err
	}
	pane, err := driver.CapturePane(id)
	if err != nil {
		return err
	}
	if !uifocus.CaretAtPromptHead(cursor, engine.Plain(sess.Tool, pane), status.PromptRules{Engine: engine, Tool: sess.Tool}) {
		return ErrNotAtPromptHead
	}
	return nil
}

func paneCursor(driver *tmux.Driver, id string) (uifocus.Cursor, error) {
	out, err := driver.Display(id, "#{cursor_x},#{cursor_y},#{cursor_flag}")
	if err != nil {
		return uifocus.Cursor{}, err
	}
	fields := strings.Split(strings.TrimSpace(out), ",")
	if len(fields) != 3 {
		return uifocus.Cursor{}, errors.New("tmux reported no cursor: " + out)
	}
	x, errX := strconv.Atoi(fields[0])
	y, errY := strconv.Atoi(fields[1])
	if errX != nil || errY != nil {
		return uifocus.Cursor{}, errors.New("tmux reported no cursor: " + out)
	}
	return uifocus.Cursor{
		X: x, Y: y,
		Visible:       !tmux.ReportsCursorFlag || fields[2] == "1",
		PositionKnown: true,
	}, nil
}
