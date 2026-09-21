package federation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func paneTarget(id string) string { return "am_" + id + ":^.0" }

type PaneState struct {
	Output                            string
	CursorX, CursorY                  int
	CursorVisible, Mouse, Motion, SGR bool
	History, Width, Height            int
}

func (c *Client) paneHost(r Ref) (Host, error) {
	h, err := c.host(r.Host)
	if err != nil {
		return h, err
	}
	if h.SSH == "" || !safeID.MatchString(r.ID) {
		return h, fmt.Errorf("invalid remote pane")
	}
	h.Binary = "tmux"
	return h, nil
}

// CaptureState carries the same cursor, mouse, and history facts as the local
// focus watcher. One tmux command list keeps metadata and capture close together.
func (c *Client) CaptureState(ctx context.Context, r Ref, offset, rows int) (PaneState, error) {
	h, err := c.paneHost(r)
	if err != nil {
		return PaneState{}, err
	}
	if offset < 0 || rows < 0 {
		return PaneState{}, fmt.Errorf("negative pane capture region")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	target := paneTarget(r.ID)
	format := "#{cursor_x},#{cursor_y},#{cursor_flag},#{mouse_any_flag}#{mouse_button_flag}#{mouse_standard_flag},#{history_size},#{mouse_all_flag},#{mouse_sgr_flag},#{pane_width},#{pane_height}"
	args := []string{"-L", "agentmgr", "display-message", "-p", "-t", target, format, ";", "capture-pane", "-p", "-e", "-t", target}
	if offset > 0 {
		args = append(args, "-S", strconv.Itoa(-(offset + rows)), "-E", "-")
	}
	b, err := c.rpc(ctx, h, args...)
	if err != nil {
		return PaneState{}, err
	}
	line, output, ok := strings.Cut(string(b), "\n")
	if !ok {
		return PaneState{}, fmt.Errorf("missing remote pane metadata")
	}
	fields := strings.Split(strings.TrimSpace(line), ",")
	if len(fields) != 9 {
		return PaneState{}, fmt.Errorf("invalid remote pane metadata %q", line)
	}
	state := PaneState{Output: output, CursorVisible: fields[2] == "1", Mouse: strings.Contains(fields[3], "1"), Motion: fields[5] == "1", SGR: fields[6] == "1"}
	for _, field := range []struct {
		i   int
		dst *int
	}{{0, &state.CursorX}, {1, &state.CursorY}, {4, &state.History}, {7, &state.Width}, {8, &state.Height}} {
		*field.dst, err = strconv.Atoi(fields[field.i])
		if err != nil {
			return PaneState{}, fmt.Errorf("invalid remote pane numeric metadata: %w", err)
		}
	}
	if offset > 0 {
		lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
		end := len(lines) - offset
		if end < 0 {
			end = 0
		}
		start := end - rows
		if start < 0 {
			start = 0
		}
		state.Output = strings.Join(lines[start:end], "\n") + "\n"
		state.CursorVisible = false
	}
	return state, nil
}

// Resize matches the local driver's split-pane geometry instead of shrinking a
// teammate pane out from under the agent.
func (c *Client) Resize(ctx context.Context, r Ref, width, height int) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	h, err := c.paneHost(r)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	b, err := c.rpc(ctx, h, "-L", "agentmgr", "display-message", "-p", "-t", paneTarget(r.ID), "#{window_panes} #{window_width} #{window_height} #{pane_width} #{pane_height}")
	if err != nil {
		return err
	}
	var panes, ww, wh, pw, ph int
	if _, err = fmt.Sscanf(strings.TrimSpace(string(b)), "%d %d %d %d %d", &panes, &ww, &wh, &pw, &ph); err != nil {
		return fmt.Errorf("invalid remote pane geometry: %w", err)
	}
	if panes > 1 {
		ww = width + (ww - pw)
		wh = height + (wh - ph)
	} else {
		ww, wh = width, height
	}
	args := []string{"-L", "agentmgr", "resize-window", "-t", "am_" + r.ID + ":^", "-x", strconv.Itoa(ww), "-y", strconv.Itoa(wh)}
	if panes > 1 {
		args = append(args, ";", "resize-pane", "-t", paneTarget(r.ID), "-x", strconv.Itoa(width), "-y", strconv.Itoa(height))
	}
	_, err = c.rpc(ctx, h, args...)
	return err
}

// SendMouse asks tmux to recheck mouse ownership at delivery, so an application
// exiting mouse mode cannot leave an escape report typed into its shell prompt.
func (c *Client) SendMouse(ctx context.Context, r Ref, report string) error {
	h, err := c.paneHost(r)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(report, "\x1b[") {
		return fmt.Errorf("invalid mouse report")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	hex := make([]string, len(report))
	for i, b := range []byte(report) {
		hex[i] = fmt.Sprintf("%02x", b)
	}
	send := "send-keys -t " + paneTarget(r.ID) + " -H " + strings.Join(hex, " ")
	_, err = c.rpc(ctx, h, "-L", "agentmgr", "if-shell", "-F", "-t", paneTarget(r.ID), "#{mouse_any_flag}", send)
	return err
}
