package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"runtime/debug"
	"testing"
)

func TestPrintHelpDoesNotRequireATerminal(t *testing.T) {
	var out bytes.Buffer
	if err := printHelp(&out); err != nil {
		t.Fatalf("printHelp: %v", err)
	}
	for _, want := range []string{
		"Usage: agent-manager [command]",
		"Run the interactive manager when no command is given.",
		"-h, --help",
		"-v, --version",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help text does not contain %q:\n%s", want, out.String())
		}
	}
}

type failingHelpWriter struct{}

func (failingHelpWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestPrintHelpReturnsWriteError(t *testing.T) {
	if err := printHelp(failingHelpWriter{}); err == nil {
		t.Fatal("printHelp succeeded after the writer failed")
	}
}

func TestMainPrintsHelpWithoutStartingTUI(t *testing.T) {
	if os.Getenv("AGENT_MANAGER_HELP_TEST") == "1" {
		flag := os.Args[len(os.Args)-1]
		os.Args = []string{"agent-manager", flag}
		main()
		return
	}
	for _, flag := range []string{"--help", "-h"} {
		t.Run(flag, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=TestMainPrintsHelpWithoutStartingTUI", "--", flag)
			cmd.Env = append(os.Environ(), "AGENT_MANAGER_HELP_TEST=1")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("agent-manager %s: %v\n%s", flag, err, out)
			}
			if !strings.Contains(string(out), "Usage: agent-manager [command]") {
				t.Fatalf("agent-manager %s did not print help:\n%s", flag, out)
			}
		})
	}
}

// Startup is the only place the alternate-scroll reset goes out, and it
// cannot be exercised headlessly: run() takes over the terminal. Reading
// the call out of the syntax tree still fails if it is dropped, which is
// what would put wheel notches back on the session cursor (#110).
func TestStartupDisablesAlternateScroll(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	var run *ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "run" && fn.Recv == nil {
			run = fn
		}
	}
	if run == nil {
		t.Fatal("main.go has no run function")
	}
	found := false
	ast.Inspect(run, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "DisableAlternateScroll" {
			return true
		}
		if pkg, ok := selector.X.(*ast.Ident); ok && pkg.Name == "ui" {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("run() never calls ui.DisableAlternateScroll")
	}
}

// Without mouse reporting the terminal keeps the wheel and scrolls the
// manager out of view, which is what alternate scroll used to prevent.
func TestStartupClaimsMouse(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "NewProgram" {
			return true
		}
		for _, arg := range call.Args {
			option, ok := arg.(*ast.CallExpr)
			if !ok {
				continue
			}
			name, ok := option.Fun.(*ast.SelectorExpr)
			if ok && name.Sel.Name == "WithMouseCellMotion" {
				found = true
			}
		}
		return true
	})
	if !found {
		t.Fatal("the program starts without mouse reporting")
	}
}

func TestResolveVersion(t *testing.T) {
	cases := []struct {
		label         string
		embedded      string
		moduleVersion string
		hasInfo       bool
		want          string
	}{
		{"ldflags win", "0.11.0", "v0.11.0", true, "0.11.0"},
		{"ldflags win over missing info", "0.11.0", "", false, "0.11.0"},
		{"go install at a tag", devVersion, "v0.11.0", true, "0.11.0"},
		{"pseudo-version", devVersion, "v0.10.6-0.20260730153639-3b5b8a9a5649", true, devVersion},
		{"go run", devVersion, "(devel)", true, devVersion},
		{"empty module version", devVersion, "", true, devVersion},
		{"no build info", devVersion, "", false, devVersion},
		{"two components", devVersion, "v0.11", true, devVersion},
		{"non-numeric", devVersion, "vX.Y.Z", true, devVersion},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			var info *debug.BuildInfo
			if tc.hasInfo {
				info = &debug.BuildInfo{Main: debug.Module{Version: tc.moduleVersion}}
			}
			if got := resolveVersion(tc.embedded, info, tc.hasInfo); got != tc.want {
				t.Fatalf("resolveVersion(%q, %q) = %q, want %q", tc.embedded, tc.moduleVersion, got, tc.want)
			}
		})
	}
}
