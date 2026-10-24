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
	helpBoundary            = boundary{"Help", "/internal/ui/help", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/help", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/theme"}}
	surfaceBoundary         = boundary{"Surface", "/internal/ui/surface", []string{"/internal/ui/surface"}}
	diffBoundary            = boundary{"Diff model", "/internal/diff/model", []string{"/internal/diff/model", "/internal/git/value"}}
	reviewBoundary          = boundary{"Review", "/internal/ui/review", []string{"/internal/diff/model", "/internal/git/value", "/internal/ui/presentation", "/internal/ui/review", "/internal/ui/surface"}}
	focusBoundary           = boundary{"Focus", "/internal/ui/focus", []string{"/internal/catalog/value", "/internal/deps", "/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/focus", "/internal/ui/frame", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface", "/internal/ui/theme"}}
	watchBoundary           = boundary{"Focus watch", "/internal/ui/focus/watch", []string{"/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/tmux", "/internal/ui/focus/watch"}}
	railBoundary            = boundary{"Rail", "/internal/ui/rail", []string{"/internal/keybind", "/internal/remote/value", "/internal/systheme", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/rail", "/internal/ui/theme"}}
	settingsBoundary        = boundary{"Settings", "/internal/ui/settings", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/settings", "/internal/ui/surface", "/internal/ui/theme"}}
	geometryBoundary        = boundary{"Geometry", "/internal/ui/geometry", []string{"/internal/keybind", "/internal/ui/geometry", "/internal/ui/surface"}}
	terminalBoundary        = boundary{"Terminal", "/internal/ui/terminal", []string{"/internal/atomicfile", "/internal/clipboard", "/internal/deps", "/internal/keybind", "/internal/termseq", "/internal/ui/presentation", "/internal/ui/session", "/internal/ui/terminal", "/internal/update", "/internal/wsl"}}
	themeBoundary           = boundary{"Theme", "/internal/ui/theme", []string{"/internal/systheme", "/internal/ui/effects", "/internal/ui/theme"}}
	spawnBoundary           = boundary{"Spawn", "/internal/ui/spawn", []string{"/internal/catalog/value", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface"}}
	composerBoundary        = boundary{"Composer", "/internal/ui/composer", []string{"/internal/catalog/value", "/internal/clipboard", "/internal/termseq", "/internal/ui/composer", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface", "/internal/wsl"}}
	pathBoundary            = boundary{"Path complete", "/internal/ui/pathcomplete", []string{"/internal/ui/pathcomplete", "/internal/ui/presentation"}}
	quickBoundary           = boundary{"Quick", "/internal/ui/quick", []string{"/internal/catalog/value", "/internal/clipboard", "/internal/termseq", "/internal/ui/composer", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/quick", "/internal/ui/spawn", "/internal/ui/surface", "/internal/wsl"}}
	quickSendBoundary       = boundary{"Quick send", "/internal/ui/quick/send", []string{"/internal/atomicfile", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/termseq", "/internal/tmux", "/internal/ui/composer", "/internal/ui/effects", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/quick/send", "/internal/ui/session", "/internal/ui/spawn", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	dialogsBoundary         = boundary{"Dialogs", "/internal/ui/dialogs", []string{"/internal/catalog/value", "/internal/ui/dialogs", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface"}}
	noticesBoundary         = boundary{"Notices", "/internal/ui/notices", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/notices", "/internal/ui/presentation", "/internal/ui/surface", "/internal/ui/theme"}}
	releaseBoundary         = boundary{"Notices release", "/internal/ui/notices/release", []string{"/internal/atomicfile", "/internal/clipboard", "/internal/config", "/internal/deps", "/internal/feed", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/systheme", "/internal/termseq", "/internal/tmux", "/internal/ui/effects", "/internal/ui/notices", "/internal/ui/notices/release", "/internal/ui/presentation", "/internal/ui/session", "/internal/ui/surface", "/internal/ui/terminal", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	valueBoundary           = boundary{"Git values", "/internal/git/value", []string{"/internal/git/value"}}
	catalogBoundary         = boundary{"Catalog values", "/internal/catalog/value", []string{"/internal/catalog/value"}}
	presentBoundary         = boundary{"Presentation", "/internal/ui/presentation", []string{"/internal/ui/presentation"}}
	connectionsBoundary     = boundary{"Connections", "/internal/ui/connections", []string{"/internal/atomicfile", "/internal/catalog/value", "/internal/deps", "/internal/keybind", "/internal/remote/value", "/internal/systheme", "/internal/ui/connections", "/internal/ui/dialogs", "/internal/ui/effects", "/internal/ui/frame", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/preview", "/internal/ui/rail", "/internal/ui/spawn", "/internal/ui/surface", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	effectsBoundary         = boundary{"Effects", "/internal/ui/effects", []string{"/internal/ui/effects"}}
	frameBoundary           = boundary{"Frame", "/internal/ui/frame", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/frame", "/internal/ui/presentation", "/internal/ui/theme"}}
	launchhintBoundary      = boundary{"Launch hint", "/internal/ui/launchhint", []string{"/internal/ui/launchhint"}}
	previewBoundary         = boundary{"Preview", "/internal/ui/preview", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/frame", "/internal/ui/presentation", "/internal/ui/preview", "/internal/ui/theme"}}
	sessionBoundary         = boundary{"Session", "/internal/ui/session", []string{"/internal/atomicfile", "/internal/deps", "/internal/keybind", "/internal/ui/session", "/internal/update", "/internal/wsl"}}
	startupBoundary         = boundary{"Startup", "/internal/ui/startup", []string{"/internal/pwsh", "/internal/systheme", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/startup", "/internal/ui/theme"}}
	statusbarBoundary       = boundary{"Status bar", "/internal/ui/statusbar", []string{"/internal/systheme", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/statusbar", "/internal/ui/theme"}}
	workspaceBoundary       = boundary{"Workspace", "/internal/ui/workspace", []string{"/internal/keybind", "/internal/systheme", "/internal/ui/effects", "/internal/ui/frame", "/internal/ui/presentation", "/internal/ui/theme", "/internal/ui/workspace"}}
	remoteBoundary          = boundary{"Remote values", "/internal/remote/value", []string{"/internal/remote/value"}}
	settingsPersistBoundary = boundary{"Settings persist", "/internal/ui/settings/persist", []string{"/internal/atomicfile", "/internal/config", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/systheme", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/settings", "/internal/ui/settings/persist", "/internal/ui/surface", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	installBoundary         = boundary{"Launch hint install", "/internal/ui/launchhint/install", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/ui/composer", "/internal/ui/effects", "/internal/ui/launchhint", "/internal/ui/launchhint/install", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	railPersistBoundary     = boundary{"Rail persist", "/internal/ui/rail/persist", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/remote/value", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/systheme", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/rail", "/internal/ui/rail/persist", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	dialogsApplyBoundary    = boundary{"Dialogs apply", "/internal/ui/dialogs/apply", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/tmux", "/internal/ui/dialogs/apply", "/internal/ui/effects", "/internal/update", "/internal/wsl"}}
	focusRuntimeBoundary    = boundary{"Focus runtime", "/internal/ui/focus/runtime", []string{"/internal/atomicfile", "/internal/catalog/value", "/internal/config", "/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/systheme", "/internal/tmux", "/internal/ui/effects", "/internal/ui/focus", "/internal/ui/focus/runtime", "/internal/ui/focus/watch", "/internal/ui/frame", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/session", "/internal/ui/spawn", "/internal/ui/surface", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
	captureBoundary         = boundary{"Preview capture", "/internal/ui/preview/capture", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/execution", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/notify", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/ui/preview/capture", "/internal/update", "/internal/wsl"}}
	panesBoundary           = boundary{"Geometry panes", "/internal/ui/geometry/panes", []string{"/internal/deps", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/tmux", "/internal/ui/effects", "/internal/ui/geometry", "/internal/ui/geometry/panes", "/internal/ui/surface"}}
	reviewSourceBoundary    = boundary{"Review source", "/internal/ui/review/source", []string{"/internal/atomicfile", "/internal/config", "/internal/deps", "/internal/diff", "/internal/diff/model", "/internal/git", "/internal/git/value", "/internal/keybind", "/internal/proctree", "/internal/pwsh", "/internal/status", "/internal/store", "/internal/tmux", "/internal/ui/effects", "/internal/ui/presentation", "/internal/ui/review", "/internal/ui/review/source", "/internal/ui/session", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	lifecycleBoundary       = boundary{"Session lifecycle", "/internal/ui/session/lifecycle", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/tmux", "/internal/ui/effects", "/internal/ui/session", "/internal/ui/session/lifecycle", "/internal/update", "/internal/wsl"}}
	spawnLaunchBoundary     = boundary{"Spawn launch", "/internal/ui/spawn/launch", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/termseq", "/internal/tmux", "/internal/ui/composer", "/internal/ui/effects", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/spawn", "/internal/ui/spawn/launch", "/internal/ui/surface", "/internal/update", "/internal/wsl"}}
	linkBoundary            = boundary{"Connections link", "/internal/ui/connections/link", []string{"/internal/agentsession", "/internal/atomicfile", "/internal/catalog", "/internal/catalog/value", "/internal/clipboard", "/internal/config", "/internal/conversation", "/internal/deps", "/internal/git", "/internal/git/value", "/internal/hooks", "/internal/keybind", "/internal/launch", "/internal/mcpreg", "/internal/proctree", "/internal/pwsh", "/internal/remote", "/internal/remote/value", "/internal/sessioncmd", "/internal/sessionreport", "/internal/status", "/internal/store", "/internal/sysstat", "/internal/systheme", "/internal/termseq", "/internal/tmux", "/internal/ui/connections", "/internal/ui/connections/link", "/internal/ui/dialogs", "/internal/ui/effects", "/internal/ui/frame", "/internal/ui/pathcomplete", "/internal/ui/presentation", "/internal/ui/preview", "/internal/ui/rail", "/internal/ui/session", "/internal/ui/spawn", "/internal/ui/surface", "/internal/ui/terminal", "/internal/ui/theme", "/internal/update", "/internal/wsl"}}
)

func main() {
	for _, feature := range []boundary{helpBoundary, reviewBoundary, focusBoundary, watchBoundary, railBoundary, settingsBoundary, geometryBoundary, terminalBoundary, effectsBoundary, frameBoundary, launchhintBoundary, previewBoundary, sessionBoundary, startupBoundary, statusbarBoundary, workspaceBoundary, themeBoundary, spawnBoundary, quickBoundary, pathBoundary, composerBoundary, dialogsBoundary, noticesBoundary, releaseBoundary, connectionsBoundary, linkBoundary, settingsPersistBoundary, installBoundary, railPersistBoundary, quickSendBoundary, dialogsApplyBoundary, focusRuntimeBoundary, captureBoundary, panesBoundary, reviewSourceBoundary, lifecycleBoundary, spawnLaunchBoundary, surfaceBoundary, diffBoundary, valueBoundary, catalogBoundary, presentBoundary, remoteBoundary} {
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
