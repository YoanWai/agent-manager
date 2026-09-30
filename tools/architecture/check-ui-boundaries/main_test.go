package main

import "testing"

func TestHelpDependencyGraph(t *testing.T) {
	if err := checkHelpGraph(); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureDependenciesPermitOnlyPresentationAndBindings(t *testing.T) {
	deps := []string{"fmt", "github.com/charmbracelet/bubbletea", module + "/internal/ui/help", module + "/internal/keybind", module + "/internal/ui/presentation"}
	if err := checkDependencies(deps); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureDependenciesRejectRootAndRuntime(t *testing.T) {
	for _, path := range []string{"", "/internal/ui", "/internal/store", "/internal/tmux", "/internal/execution", "/internal/sessioncmd", "/internal/config", "/internal/ui/review", "/pkg/runtime", "/examples/bridge", "/tools/commands"} {
		t.Run(path, func(t *testing.T) {
			if err := checkDependencies([]string{module + path}); err == nil {
				t.Fatalf("forbidden dependency %s was accepted", module+path)
			}
		})
	}
}
