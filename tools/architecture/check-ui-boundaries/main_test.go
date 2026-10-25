package main

import (
	"os/exec"
	"strings"
	"testing"
)

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

func TestRemoteValuesRejectSSH(t *testing.T) {
	if err := checkBoundary(remoteBoundary); err != nil {
		t.Fatal(err)
	}
	if err := checkBoundaryDependencies(remoteBoundary, []string{module + "/internal/remote"}); err == nil {
		t.Fatal("remote values accepted the SSH client")
	}
}

func TestFeaturePackageGraphs(t *testing.T) {
	for _, feature := range boundaries {
		t.Run(feature.name, func(t *testing.T) {
			if err := checkBoundary(feature); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestFeaturePackagesRejectRuntimeTransitively(t *testing.T) {
	for _, feature := range boundaries {
		if feature.runtime {
			continue
		}
		t.Run(feature.name, func(t *testing.T) {
			for _, path := range []string{"/internal/ui", "/internal/git", "/internal/store", "/internal/tmux", "/internal/execution", "/internal/app", "/internal/status", "/internal/catalog", "/internal/config", "/internal/remote", "/internal/sessioncmd"} {
				if err := checkBoundaryDependencies(feature, []string{module + feature.path, module + path}); err == nil {
					t.Fatalf("forbidden dependency %s was accepted", module+path)
				}
			}
		})
	}
}

// The pane watcher runs tmux and nothing else of the module: not the root,
// the store, or Focus's own model.
func TestFocusWatchStaysOnTmux(t *testing.T) {
	if err := checkBoundary(watchBoundary); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/internal/ui", "/internal/ui/focus", "/internal/store", "/internal/status", "/internal/execution"} {
		if err := checkBoundaryDependencies(watchBoundary, []string{module + path}); err == nil {
			t.Fatalf("forbidden dependency %s was accepted", module+path)
		}
	}
}

func TestCatalogValuesDependOnNothingInTheModule(t *testing.T) {
	if err := checkBoundary(catalogBoundary); err != nil {
		t.Fatal(err)
	}
}

func TestPresentationDependsOnNothingInTheModule(t *testing.T) {
	if err := checkBoundary(presentBoundary); err != nil {
		t.Fatal(err)
	}
}

func TestThemeDependenciesStayWithinBoundary(t *testing.T) {
	if err := checkBoundary(themeBoundary); err != nil {
		t.Fatal(err)
	}
	if err := checkBoundaryDependencies(themeBoundary, []string{module + "/internal/tmux"}); err == nil {
		t.Fatal("the palette accepted a runtime dependency")
	}
}

// A feature package that reaches a runtime package fails even when its list
// names it, so -write cannot quietly widen a boundary.
func TestFeatureBoundaryRefusesRuntimeEvenWhenListed(t *testing.T) {
	feature := boundary{name: "Feature", path: "/internal/ui/x", allowed: []string{"/internal/ui/x", "/internal/tmux"}}
	if err := checkBoundaryDependencies(feature, []string{module + "/internal/tmux"}); err == nil {
		t.Fatal("a listed runtime import was accepted in a feature package")
	}
	feature.runtime = true
	if err := checkBoundaryDependencies(feature, []string{module + "/internal/tmux"}); err != nil {
		t.Fatalf("a runtime package was refused its listed import: %v", err)
	}
}

// Every name boundaryNames carries is the variable at the same index, so the
// rewrite keeps each line under its own name.
func TestBoundaryNamesMatchTheList(t *testing.T) {
	if len(boundaryNames) != len(boundaries) {
		t.Fatalf("%d names for %d boundaries", len(boundaryNames), len(boundaries))
	}
}

// Every package under internal/ui has a boundary, so a new one cannot land
// unchecked.
func TestEveryUIPackageHasABoundary(t *testing.T) {
	output, err := exec.Command("go", "list", module+"/internal/ui/...").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	covered := map[string]bool{module + "/internal/ui": true}
	for _, feature := range boundaries {
		covered[module+feature.path] = true
	}
	for _, path := range strings.Fields(string(output)) {
		if !covered[path] {
			t.Errorf("%s has no boundary; add it to boundaries.go and run -write", path)
		}
	}
}
