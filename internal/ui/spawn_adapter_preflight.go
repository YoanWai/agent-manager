package ui

import (
	"github.com/YoanWai/agent-manager/internal/git"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
)

// directoryPreflight is the read-only filesystem and repository seam a spawn
// or group creation captures before it is queued. ui/spawn/launch declares the
// same seam; preflightReader crosses between them.
type directoryPreflight interface {
	resolve(raw string, fallbacks []string) (string, bool)
	repoCapable(dir string) bool
}

type preflightReader struct{ reader directoryPreflight }

func (p preflightReader) Resolve(raw string, fallbacks []string) (string, bool) {
	return p.reader.resolve(raw, fallbacks)
}

func (p preflightReader) RepoCapable(dir string) bool { return p.reader.repoCapable(dir) }

// crossPreflight hands a captured reader to the package, nil staying nil so the
// package resolves against the filesystem.
func crossPreflight(reader directoryPreflight) uilaunch.DirectoryPreflight {
	if reader == nil {
		return nil
	}
	return preflightReader{reader: reader}
}

func crossTerminalReader(reader terminalDirectoryReader) uilaunch.TerminalDirectoryReader {
	if reader == nil {
		return nil
	}
	return terminalReader{reader: reader}
}

type terminalReader struct{ reader terminalDirectoryReader }

func (t terminalReader) Resolve(request uilaunch.TerminalDirectoryRequest) (string, bool) {
	return t.reader.resolve(terminalDirectoryRequest{paneID: request.PaneID, rawDir: request.RawDir, fallbacks: request.Fallbacks})
}

// systemDirectoryPreflight resolves against the filesystem and asks git
// whether a directory sits inside a repository.
type systemDirectoryPreflight struct{ git *git.Driver }

func (p systemDirectoryPreflight) resolve(raw string, fallbacks []string) (string, bool) {
	return uilaunch.SystemDirectoryPreflight{Git: p.git}.Resolve(raw, fallbacks)
}

func (p systemDirectoryPreflight) repoCapable(dir string) bool {
	return uilaunch.SystemDirectoryPreflight{Git: p.git}.RepoCapable(dir)
}

// resolveExistingDir turns raw field input into a usable directory: expand ~,
// fall back when empty, absolutize, and require it to exist.
func resolveExistingDir(raw, fallback string) (string, bool) {
	return uilaunch.ResolveExistingDir(raw, fallback)
}

func isDir(path string) bool { return uilaunch.IsDir(path) }

// initialWorkingDir and initialHomeDir capture where the manager started,
// before the event loop reads them.
func initialWorkingDir() string { return uilaunch.InitialWorkingDir() }

func initialHomeDir() string { return uilaunch.InitialHomeDir() }
