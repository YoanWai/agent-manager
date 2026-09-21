package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	binary := flag.String("binary", "agent-manager", "Agent Manager executable")
	caller := flag.String("caller", "", "existing local managed session")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.Command(*binary, "mcp")
	cmd.Env = append(os.Environ(), "AGENT_MANAGER_SESSION_ID="+*caller)
	client := mcp.NewClient(&mcp.Implementation{Name: "native-host-smoke", Version: "1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		fail(err)
	}
	defer session.Close()
	for _, name := range []string{"list_sessions", "list_groups", "list_terminals"} {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil {
			fail(err)
		}
		if result.IsError {
			fail(fmt.Errorf("%s: %v", name, result.Content))
		}
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"tool": name, "result": result}); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
