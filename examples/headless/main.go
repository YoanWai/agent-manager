package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/YoanWai/agent-manager/examples/extensions"
	"github.com/YoanWai/agent-manager/internal/app"
	"github.com/YoanWai/agent-manager/internal/execution"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type observation struct {
	ListedAt time.Time `json:"listed_at"`
	Sessions int       `json:"sessions"`
	Backlog  []string  `json:"backlog,omitempty"`
}

func observe(ctx context.Context, profileDir string, driver *tmux.Driver, output io.Writer) (err error) {
	local, err := app.OpenLocal(profileDir, driver)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, local.Close()) }()
	ctx, cancel := context.WithCancel(ctx)
	results := local.Execution.Run(ctx)
	defer func() {
		cancel()
		for range results {
		}
	}()
	encoder := json.NewEncoder(output)
	for result := range results {
		if result.Err != nil {
			return result.Err
		}
		view := execution.ReadOnly(result.Snapshot)
		count := 0
		for range view.Sessions() {
			count++
		}
		if err := encoder.Encode(observation{ListedAt: view.ListedAt(), Sessions: count, Backlog: extensions.DeliveryBacklog(view, 1)}); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	profile := flag.String("profile", "", "explicit profile directory to observe")
	socket := flag.String("socket", "", "explicit tmux server name")
	flag.Parse()
	if *profile == "" || *socket == "" {
		fmt.Fprintln(os.Stderr, "--profile and --socket are required")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	driver, err := tmux.NewWithSocket(*socket)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := observe(ctx, *profile, driver, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
