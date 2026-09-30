package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// rpcClient speaks JSON-RPC 2.0 over a process's stdin and stdout, one
// message per line: codex's app server, muse serve and ACP agents.
type rpcClient struct {
	proc   *process
	lastID int
}

// clientInfo names this client to the servers that ask. Muse takes only a
// name of lowercase letters, digits and underscores.
var clientInfo = map[string]string{"name": "agent_manager", "version": "catalog"}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

func (c *rpcClient) write(message map[string]any) error {
	message["jsonrpc"] = "2.0"
	line, err := json.Marshal(message)
	if err != nil {
		return err
	}
	return c.proc.send(line)
}

// initialize opens the session codex's app server and muse serve both start
// with.
func (c *rpcClient) initialize(ctx context.Context) error {
	var initialized struct{}
	if err := c.call(ctx, "initialize", map[string]any{"clientInfo": clientInfo}, &initialized); err != nil {
		return err
	}
	return c.write(map[string]any{"method": "initialized"})
}

// call sends a request and decodes its result into out. A request the
// server makes meanwhile is refused, since a catalog client serves none.
func (c *rpcClient) call(ctx context.Context, method string, params, out any) error {
	c.lastID++
	id := strconv.Itoa(c.lastID)
	if err := c.write(map[string]any{"id": c.lastID, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		line, err := c.proc.next(ctx)
		if err != nil {
			return err
		}
		var message rpcMessage
		if json.Unmarshal(line, &message) != nil || message.ID == nil {
			continue
		}
		if message.Method != "" {
			if err := c.write(map[string]any{"id": message.ID, "error": rpcError{Code: -32601, Message: "not supported"}}); err != nil {
				return err
			}
			continue
		}
		if string(bytes.TrimSpace(message.ID)) != id {
			continue
		}
		if message.Error != nil {
			return fmt.Errorf("%s: %s", method, message.Error.Message)
		}
		if err := json.Unmarshal(message.Result, out); err != nil {
			return fmt.Errorf("%s: %w", method, err)
		}
		return nil
	}
}
