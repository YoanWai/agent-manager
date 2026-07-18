// Package git shells out to the git CLI to read diff data for a repo.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var ErrNotARepo = errors.New("not a git repository")

// emptyTree is git's well-known empty-tree object, used as the base for
// the first commit in a repo.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

type Driver struct {
	bin string
}

func New() (*Driver, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, fmt.Errorf("git not found in PATH: %w", err)
	}
	return &Driver{bin: bin}, nil
}

func (d *Driver) run(dir string, args ...string) (string, error) {
	cmd := exec.Command(d.bin, append([]string{"-c", "core.quotepath=false"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimRight(string(out), "\n")
	if err != nil {
		if strings.Contains(text, "not a git repository") {
			return "", ErrNotARepo
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, text)
	}
	return text, nil
}

type Scope int

const (
	ScopeUncommitted Scope = iota
	ScopeBranch
	ScopeLastCommit
	ScopeStaged
	scopeCount
)

func (s Scope) Next() Scope { return (s + 1) % scopeCount }

func (s Scope) String() string {
	switch s {
	case ScopeBranch:
		return "vs base"
	case ScopeLastCommit:
		return "last commit"
	case ScopeStaged:
		return "staged"
	default:
		return "uncommitted"
	}
}

type Repo struct {
	Root     string
	Branch   string
	Unborn   bool
	Detached bool
}

func (d *Driver) OpenRepo(dir string) (Repo, error) {
	root, err := d.run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, err
	}
	repo := Repo{Root: root}
	branch, err := d.run(root, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		repo.Detached = true
		branch, _ = d.run(root, "rev-parse", "--short", "HEAD")
	}
	repo.Branch = branch
	if _, err := d.run(root, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		repo.Unborn = true
		repo.Detached = false
	}
	return repo, nil
}

// BaseRef finds the merge base against the repo's main branch for the
// branch scope, returning the resolved ref and a short description.
func (d *Driver) BaseRef(root string) (ref, describe string, err error) {
	candidate := ""
	if out, err := d.run(root, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		candidate = out
	} else {
		for _, name := range []string{"main", "master", "develop"} {
			if _, err := d.run(root, "rev-parse", "--verify", "-q", name+"^{commit}"); err == nil {
				candidate = name
				break
			}
		}
	}
	if candidate == "" {
		if out, err := d.run(root, "rev-parse", "--abbrev-ref", "-q", "@{upstream}"); err == nil && out != "" {
			candidate = out
		}
	}
	if candidate == "" {
		return "", "", errors.New("no base branch (main/master/origin) found")
	}
	base, err := d.run(root, "merge-base", candidate, "HEAD")
	if err != nil {
		return "", "", err
	}
	short := base
	if len(short) > 7 {
		short = short[:7]
	}
	return base, candidate + "@" + short, nil
}

type Status byte

const (
	Added     Status = 'A'
	Modified  Status = 'M'
	Deleted   Status = 'D'
	Renamed   Status = 'R'
	Copied    Status = 'C'
	Untracked Status = '?'
	Unmerged  Status = 'U'
)

type ChangedFile struct {
	Path    string
	OldPath string
	Status  Status
}

// diffRange returns the base ref and diff arguments for a scope; baseRef
// is only consulted for ScopeBranch.
func (d *Driver) diffRange(root string, scope Scope, baseRef string) (base string, args []string, err error) {
	switch scope {
	case ScopeStaged:
		return "", []string{"--cached"}, nil
	case ScopeLastCommit:
		parent := "HEAD~1"
		if _, err := d.run(root, "rev-parse", "--verify", "-q", "HEAD~1"); err != nil {
			parent = emptyTree
		}
		return parent, []string{parent, "HEAD"}, nil
	case ScopeBranch:
		return baseRef, []string{baseRef, "HEAD"}, nil
	default:
		return "HEAD", []string{"HEAD"}, nil
	}
}

func (d *Driver) ChangedFiles(root string, scope Scope, baseRef string) ([]ChangedFile, error) {
	_, rangeArgs, err := d.diffRange(root, scope, baseRef)
	if err != nil {
		return nil, err
	}
	args := append([]string{"diff", "--name-status", "-z", "-M"}, rangeArgs...)
	out, err := d.run(root, args...)
	if err != nil {
		return nil, err
	}
	files := parseNameStatus(out)
	if scope == ScopeUncommitted {
		untracked, err := d.run(root, "ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return nil, err
		}
		for _, path := range splitNUL(untracked) {
			files = append(files, ChangedFile{Path: path, OldPath: path, Status: Untracked})
		}
	}
	return files, nil
}

func parseNameStatus(out string) []ChangedFile {
	fields := splitNUL(out)
	var files []ChangedFile
	for i := 0; i < len(fields); i++ {
		code := fields[i]
		if code == "" {
			continue
		}
		status := Status(code[0])
		if status == Renamed || status == Copied {
			if i+2 >= len(fields) {
				break
			}
			files = append(files, ChangedFile{OldPath: fields[i+1], Path: fields[i+2], Status: status})
			i += 2
			continue
		}
		if i+1 >= len(fields) {
			break
		}
		path := fields[i+1]
		files = append(files, ChangedFile{Path: path, OldPath: path, Status: status})
		i++
	}
	return files
}

func splitNUL(out string) []string {
	var parts []string
	for _, part := range strings.Split(out, "\x00") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

type FileStat struct {
	Adds, Dels int
	Binary     bool
}

func (d *Driver) NumStat(root string, scope Scope, baseRef string) (map[string]FileStat, error) {
	_, rangeArgs, err := d.diffRange(root, scope, baseRef)
	if err != nil {
		return nil, err
	}
	args := append([]string{"diff", "--numstat", "-z", "-M"}, rangeArgs...)
	out, err := d.run(root, args...)
	if err != nil {
		return nil, err
	}
	stats := map[string]FileStat{}
	fields := splitNUL(out)
	for i := 0; i < len(fields); i++ {
		record := fields[i]
		parts := strings.SplitN(record, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		stat := FileStat{}
		if parts[0] == "-" || parts[1] == "-" {
			stat.Binary = true
		} else {
			stat.Adds, _ = strconv.Atoi(parts[0])
			stat.Dels, _ = strconv.Atoi(parts[1])
		}
		path := parts[2]
		if path == "" {
			// Rename records put "old NUL new" after the counts; the
			// new path is the next field, the one after is the source.
			if i+2 < len(fields) {
				stats[fields[i+2]] = stat
				i += 2
				continue
			}
			break
		}
		stats[path] = stat
	}
	return stats, nil
}

// ShowFile reads a file's content at a ref; a path absent at the ref
// returns empty content (the file was added since).
func (d *Driver) ShowFile(root, ref, path string) ([]byte, error) {
	cmd := exec.Command(d.bin, "-c", "core.quotepath=false", "show", ref+":"+path)
	cmd.Dir = root
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, nil
		}
		return nil, fmt.Errorf("git show %s:%s: %w: %s", ref, path, err, strings.TrimSpace(msg))
	}
	return stdout.Bytes(), nil
}

// IndexFile reads a file's staged content.
func (d *Driver) IndexFile(root, path string) ([]byte, error) {
	return d.ShowFile(root, ":0", path)
}

func (d *Driver) WorkingFile(root, path string) ([]byte, error) {
	content, err := os.ReadFile(filepath.Join(root, path))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return content, err
}

// Fingerprint hashes the repo state visible to a scope so callers can
// cheaply detect when a reload is needed.
func (d *Driver) Fingerprint(root string, scope Scope, baseRef string) (uint64, error) {
	head, _ := d.run(root, "rev-parse", "-q", "--verify", "HEAD")
	porcelain, err := d.run(root, "status", "--porcelain", "-z")
	if err != nil {
		return 0, err
	}
	hash := fnv.New64a()
	fmt.Fprintf(hash, "%d|%s|%s|%s", scope, baseRef, head, porcelain)
	if scope == ScopeUncommitted {
		// Content edits with unchanged status lines still need detection;
		// mtimes of changed files catch them.
		for _, field := range splitNUL(porcelain) {
			if len(field) > 3 {
				if info, err := os.Stat(filepath.Join(root, field[3:])); err == nil {
					fmt.Fprintf(hash, "|%d", info.ModTime().UnixNano())
				}
			}
		}
	}
	return hash.Sum64(), nil
}

// IsBinary sniffs for a NUL byte in the first 8 KiB.
func IsBinary(content []byte) bool {
	limit := len(content)
	if limit > 8192 {
		limit = 8192
	}
	return bytes.IndexByte(content[:limit], 0) >= 0
}
