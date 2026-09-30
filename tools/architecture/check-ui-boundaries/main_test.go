package main

import "testing"

func TestHelpDependencyGraph(t *testing.T) {
	if err := checkBoundary(helpBoundary); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureDependenciesPermitOnlyPresentationAndBindings(t *testing.T) {
	deps := []string{"fmt", "github.com/charmbracelet/bubbletea", module + "/internal/ui/help", module + "/internal/keybind", module + "/internal/ui/presentation"}
	if err := checkBoundaryDependencies(helpBoundary, deps); err != nil {
		t.Fatal(err)
	}
}

func TestFeatureDependenciesRejectRootAndRuntime(t *testing.T) {
	for _, path := range []string{"", "/internal/ui", "/internal/store", "/internal/tmux", "/internal/execution", "/internal/sessioncmd", "/internal/config", "/internal/ui/review", "/pkg/runtime", "/examples/bridge", "/tools/commands"} {
		t.Run(path, func(t *testing.T) {
			if err := checkBoundaryDependencies(helpBoundary, []string{module + path}); err == nil {
				t.Fatalf("forbidden dependency %s was accepted", module+path)
			}
		})
	}
}

func TestPureReviewDataDoesNotImportGitRuntime(t *testing.T) {
	if err := checkBoundary(diffBoundary); err != nil {
		t.Fatal(err)
	}
	if err := checkBoundary(valueBoundary); err != nil {
		t.Fatal(err)
	}
}

func TestPureReviewDataRejectsConcreteGit(t *testing.T) {
	if err := checkBoundaryDependencies(diffBoundary, []string{module + "/internal/git"}); err == nil {
		t.Fatal("pure diff model accepted Git runtime")
	}
}

func TestFeaturePackageGraphs(t *testing.T) {
	for _, feature := range []boundary{reviewBoundary, focusBoundary, railBoundary} {
		t.Run(feature.name, func(t *testing.T) {
			if err := checkBoundary(feature); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFeaturePackagesRejectRuntimeTransitively(t *testing.T) {
	for _, feature := range []boundary{reviewBoundary, focusBoundary, railBoundary} {
		t.Run(feature.name, func(t *testing.T) {
			for _, path := range []string{"/internal/ui", "/internal/git", "/internal/store", "/internal/tmux", "/internal/execution", "/internal/app", "/internal/status"} {
				if err := checkBoundaryDependencies(feature, []string{module + feature.path, module + path}); err == nil {
					t.Fatalf("forbidden dependency %s was accepted", module+path)
				}
			}
		})
	}
}
