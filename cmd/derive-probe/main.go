package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/charmbracelet/x/ansi"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	engine, err := status.NewEngine(cfg)
	if err != nil {
		panic(err)
	}
	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", os.Args[1]).Output()
	if err != nil {
		panic(err)
	}
	state, matched := engine.Match(os.Args[2], ansi.Strip(string(out)))
	fmt.Println(state, matched)
}
