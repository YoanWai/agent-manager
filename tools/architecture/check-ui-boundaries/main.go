package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const module = "github.com/YoanWai/agent-manager"

type boundary struct {
	name    string
	path    string
	allowed []string
}

var (
	helpBoundary    = boundary{"Help", "/internal/ui/help", []string{"/internal/ui/help", "/internal/keybind", "/internal/ui/presentation"}}
	diffBoundary    = boundary{"Diff model", "/internal/diff/model", []string{"/internal/diff/model", "/internal/git/value"}}
	reviewBoundary  = boundary{"Review", "/internal/ui/review", []string{"/internal/ui/review", "/internal/diff/model", "/internal/git/value", "/internal/ui/presentation"}}
	focusBoundary   = boundary{"Focus", "/internal/ui/focus", []string{"/internal/ui/focus"}}
	railBoundary    = boundary{"Rail", "/internal/ui/rail", []string{"/internal/ui/rail", "/internal/keybind", "/internal/ui/presentation"}}
	valueBoundary   = boundary{"Git values", "/internal/git/value", []string{"/internal/git/value"}}
	presentBoundary = boundary{"Presentation", "/internal/ui/presentation", []string{"/internal/ui/presentation"}}
)

func main() {
	for _, feature := range []boundary{helpBoundary, reviewBoundary, focusBoundary, railBoundary, diffBoundary, valueBoundary, presentBoundary} {
		if err := checkBoundary(feature); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("PASS: %s production dependencies stay within its explicit boundary\n", feature.name)
	}
}

func checkBoundary(feature boundary) error {
	command := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", module+feature.path)
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("load %s dependency graph: %w\n%s", feature.name, err, output)
	}
	return checkBoundaryDependencies(feature, strings.Fields(string(output)))
}

func checkBoundaryDependencies(feature boundary, dependencies []string) error {
	allowed := make(map[string]bool, len(feature.allowed))
	for _, path := range feature.allowed {
		allowed[module+path] = true
	}
	for _, dependency := range dependencies {
		if dependency == module {
			return fmt.Errorf("%s depends on application root %s", feature.name, dependency)
		}
		if !strings.HasPrefix(dependency, module+"/") {
			continue
		}
		if !allowed[dependency] {
			return fmt.Errorf("%s depends on forbidden package %s", feature.name, dependency)
		}
	}
	return nil
}
