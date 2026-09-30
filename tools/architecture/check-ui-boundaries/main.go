package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const module = "github.com/YoanWai/agent-manager"

func main() {
	if err := checkHelpGraph(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("PASS: Help depends only on bindings and presentation inside this repository")
}

func checkHelpGraph() error {
	command := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", module+"/internal/ui/help")
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("load Help dependency graph: %w\n%s", err, output)
	}
	return checkDependencies(strings.Fields(string(output)))
}

func checkDependencies(dependencies []string) error {
	for _, dependency := range dependencies {
		if dependency == module {
			return fmt.Errorf("Help depends on application root %s", dependency)
		}
		if !strings.HasPrefix(dependency, module+"/") {
			continue
		}
		switch dependency {
		case module + "/internal/ui/help", module + "/internal/keybind", module + "/internal/ui/presentation":
			continue
		default:
			return fmt.Errorf("Help depends on forbidden package %s", dependency)
		}
	}
	return nil
}
