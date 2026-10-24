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
	helpBoundary            = boundary{"Help", "/internal/ui/help", []string{"/internal/ui/help", "/internal/keybind", "/internal/ui/presentation", "/internal/ui/surface"}}
	surfaceBoundary         = boundary{"Surface", "/internal/ui/surface", []string{"/internal/ui/surface"}}
	diffBoundary            = boundary{"Diff model", "/internal/diff/model", []string{"/internal/diff/model", "/internal/git/value"}}
	reviewBoundary          = boundary{"Review", "/internal/ui/review", []string{"/internal/ui/review", "/internal/diff/model", "/internal/git/value", "/internal/ui/presentation", "/internal/ui/surface"}}
	focusBoundary           = boundary{"Focus", "/internal/ui/focus", []string{"/internal/ui/focus", "/internal/keybind", "/internal/ui/spawn", "/internal/catalog/value", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/pathcomplete"}}
	watchBoundary           = boundary{"Focus watch", "/internal/ui/focus/watch", []string{"/internal/ui/focus/watch", "/internal/tmux", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh"}}
	railBoundary            = boundary{"Rail", "/internal/ui/rail", []string{"/internal/ui/rail", "/internal/keybind", "/internal/remote/value", "/internal/ui/presentation", "/internal/ui/theme"}}
	settingsBoundary        = boundary{"Settings", "/internal/ui/settings", []string{"/internal/ui/settings", "/internal/keybind", "/internal/ui/presentation", "/internal/ui/theme", "/internal/ui/surface"}}
	geometryBoundary        = boundary{"Geometry", "/internal/ui/geometry", []string{"/internal/ui/geometry", "/internal/keybind", "/internal/ui/surface"}}
	terminalBoundary        = boundary{"Terminal", "/internal/ui/terminal", []string{"/internal/ui/terminal", "/internal/clipboard", "/internal/termseq", "/internal/ui/presentation", "/internal/ui/session", "/internal/wsl"}}
	themeBoundary           = boundary{"Theme", "/internal/ui/theme", []string{"/internal/ui/theme", "/internal/systheme", "/internal/ui/effects"}}
	spawnBoundary           = boundary{"Spawn", "/internal/ui/spawn", []string{"/internal/ui/spawn", "/internal/catalog/value", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/pathcomplete"}}
	composerBoundary        = boundary{"Composer", "/internal/ui/composer", []string{"/internal/ui/composer", "/internal/clipboard", "/internal/termseq", "/internal/wsl", "/internal/ui/spawn", "/internal/catalog/value", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/pathcomplete"}}
	pathBoundary            = boundary{"Path complete", "/internal/ui/pathcomplete", []string{"/internal/ui/pathcomplete", "/internal/ui/presentation"}}
	quickBoundary           = boundary{"Quick", "/internal/ui/quick", []string{"/internal/ui/quick", "/internal/ui/spawn", "/internal/catalog/value", "/internal/ui/presentation", "/internal/ui/composer", "/internal/clipboard", "/internal/termseq", "/internal/wsl", "/internal/ui/surface", "/internal/ui/pathcomplete", "/internal/atomicfile", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/tmux", "/internal/ui/effects", "/internal/ui/session", "/internal/config", "/internal/update"}}
	dialogsBoundary         = boundary{"Dialogs", "/internal/ui/dialogs", []string{"/internal/ui/dialogs", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/pathcomplete", "/internal/ui/spawn", "/internal/catalog/value"}}
	noticesBoundary         = boundary{"Notices", "/internal/ui/notices", []string{"/internal/ui/notices", "/internal/keybind", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/surface"}}
	releaseBoundary         = boundary{"Notices release", "/internal/ui/notices/release", []string{"/internal/ui/notices/release", "/internal/ui/notices", "/internal/keybind", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/terminal", "/internal/clipboard", "/internal/termseq", "/internal/wsl", "/internal/config", "/internal/deps", "/internal/tmux", "/internal/proctree", "/internal/pwsh", "/internal/update", "/internal/feed", "/internal/atomicfile"}}
	valueBoundary           = boundary{"Git values", "/internal/git/value", []string{"/internal/git/value"}}
	catalogBoundary         = boundary{"Catalog values", "/internal/catalog/value", []string{"/internal/catalog/value"}}
	presentBoundary         = boundary{"Presentation", "/internal/ui/presentation", []string{"/internal/ui/presentation"}}
	connectionsBoundary     = boundary{"Connections", "/internal/ui/connections", []string{"/internal/ui/connections", "/internal/remote/value", "/internal/ui/dialogs", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/pathcomplete", "/internal/ui/spawn", "/internal/catalog/value"}}
	effectsBoundary         = boundary{"Effects", "/internal/ui/effects", []string{"/internal/ui/effects"}}
	frameBoundary           = boundary{"Frame", "/internal/ui/frame", []string{"/internal/ui/frame", "/internal/keybind", "/internal/ui/presentation"}}
	launchhintBoundary      = boundary{"Launch hint", "/internal/ui/launchhint", []string{"/internal/ui/launchhint"}}
	previewBoundary         = boundary{"Preview", "/internal/ui/preview", []string{"/internal/ui/preview", "/internal/keybind", "/internal/ui/frame", "/internal/ui/presentation"}}
	sessionBoundary         = boundary{"Session", "/internal/ui/session", []string{"/internal/ui/session"}}
	startupBoundary         = boundary{"Startup", "/internal/ui/startup", []string{"/internal/ui/startup", "/internal/pwsh", "/internal/ui/presentation", "/internal/ui/theme"}}
	statusbarBoundary       = boundary{"Status bar", "/internal/ui/statusbar", []string{"/internal/ui/statusbar", "/internal/ui/presentation", "/internal/ui/theme"}}
	workspaceBoundary       = boundary{"Workspace", "/internal/ui/workspace", []string{"/internal/ui/workspace", "/internal/ui/frame", "/internal/ui/presentation", "/internal/ui/theme"}}
	remoteBoundary          = boundary{"Remote values", "/internal/remote/value", []string{"/internal/remote/value"}}
	settingsPersistBoundary = boundary{"Settings persist", "/internal/ui/settings/persist", []string{"/internal/ui/settings/persist", "/internal/atomicfile", "/internal/config", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/settings", "/internal/ui/surface", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	installBoundary         = boundary{"Launch hint install", "/internal/ui/launchhint/install", []string{"/internal/ui/launchhint/install", "/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/ui/composer", "/internal/ui/effects", "/internal/ui/launchhint", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	railPersistBoundary     = boundary{"Rail persist", "/internal/ui/rail/persist", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/remote/value", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/rail", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	dialogsApplyBoundary    = boundary{"Dialogs apply", "/internal/ui/dialogs/apply", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/tmux", "/internal/ui/effects", "/internal/update", "/internal/wsl"}}
	focusRuntimeBoundary    = boundary{"Focus runtime", "/internal/ui/focus/runtime", []string{"/internal/atomicfile", "/internal/catalog/value", "/internal/config", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/tmux", "/internal/ui/effects", "/internal/ui/focus", "/internal/ui/focus/watch", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/session", "/internal/ui/spawn", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	captureBoundary         = boundary{"Preview capture", "/internal/ui/preview/capture", []string{"/internal/ui/preview/capture", "/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/execution", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/notify", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/update", "/internal/wsl"}}
	panesBoundary           = boundary{"Geometry panes", "/internal/ui/geometry/panes", []string{"/internal/ui/geometry/panes", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/tmux", "/internal/ui/effects", "/internal/ui/geometry", "/internal/ui/surface"}}
	reviewSourceBoundary    = boundary{"Review source", "/internal/ui/review/source", []string{"/internal/ui/review/source", "/internal/atomicfile", "/internal/config", "/internal/deps", "/internal/diff", "/internal/diff/model", "/internal/git", "/internal/git/value", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/review", "/internal/ui/session", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	lifecycleBoundary       = boundary{"Session lifecycle", "/internal/ui/session/lifecycle", []string{"/internal/ui/session/lifecycle", "/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/tmux", "/internal/ui/effects", "/internal/ui/session", "/internal/update", "/internal/wsl"}}
	spawnLaunchBoundary     = boundary{"Spawn launch", "/internal/ui/spawn/launch", []string{"/internal/ui/spawn/launch", "/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/ui/composer", "/internal/ui/effects", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	linkBoundary            = boundary{"Connections link", "/internal/ui/connections/link", []string{
		"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value",
		"/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git",
		"/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg",
		"/internal/proctree", "/internal/pwsh", "/internal/remote", "/internal/remote/value", "/internal/sessioncmd",
		"/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq",
		"/internal/tmux", "/internal/ui/connections", "/internal/ui/connections/link", "/internal/ui/dialogs",
		"/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/rail", "/internal/ui/spawn",
		"/internal/ui/surface", "/internal/ui/terminal", "/internal/ui/theme", "/internal/update", "/internal/wsl",
		"/internal/ui/effects",
	}}
)

func main() {
	for _, feature := range []boundary{helpBoundary, reviewBoundary, focusBoundary, watchBoundary, railBoundary, settingsBoundary, geometryBoundary, terminalBoundary, effectsBoundary, frameBoundary, launchhintBoundary, previewBoundary, sessionBoundary, startupBoundary, statusbarBoundary, workspaceBoundary, themeBoundary, spawnBoundary, quickBoundary, pathBoundary, composerBoundary, dialogsBoundary, noticesBoundary, releaseBoundary, connectionsBoundary, linkBoundary, settingsPersistBoundary, installBoundary, railPersistBoundary, dialogsApplyBoundary, focusRuntimeBoundary, captureBoundary, panesBoundary, reviewSourceBoundary, lifecycleBoundary, spawnLaunchBoundary, surfaceBoundary, diffBoundary, valueBoundary, catalogBoundary, presentBoundary, remoteBoundary} {
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
