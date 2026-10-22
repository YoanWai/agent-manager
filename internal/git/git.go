// Package git shells out to the git CLI to read diff data for a repo.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/git/value"
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
		return nil, fmt.Errorf("git not found in PATH: %w\n%s", err, deps.Hint("git"))
	}
	return &Driver{bin: bin}, nil
}

// run returns git's stdout. Stderr only joins the error text: git writes
// warnings there on success (Git for Windows' default core.autocrlf=true warns
// about every LF file), and they must not corrupt parsed output.
func (d *Driver) run(dir string, args ...string) (string, error) {
	cmd := exec.Command(d.bin, append([]string{"-c", "core.quotepath=false"}, args...)...)
	cmd.Dir = dir
	// The manager's UI owns the terminal, so a credential, askpass or ssh
	// prompt from a hook or LFS filter would hang unseen; off its terminal
	// and with prompts disabled, git fails instead.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	detachTerminal(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		text := strings.TrimRight(stdout.String()+stderr.String(), "\n")
		if strings.Contains(text, "not a git repository") {
			return "", ErrNotARepo
		}
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, text)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// toplevel returns the root of the repo holding dir as a native path: git
// prints forward slashes on Windows ("C:/Users/..."), which would not match
// paths built with filepath.
func (d *Driver) toplevel(dir string) (string, error) {
	top, err := d.run(dir, "rev-parse", "--show-toplevel")
	return filepath.FromSlash(top), err
}

type Scope = value.Scope

const (
	ScopeUncommitted = value.ScopeUncommitted
	ScopeBranch      = value.ScopeBranch
	ScopeLastCommit  = value.ScopeLastCommit
	ScopeStaged      = value.ScopeStaged
)

type Repo = value.Repo

func (d *Driver) OpenRepo(dir string) (Repo, error) {
	root, err := d.toplevel(dir)
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
	head, err := d.run(root, "rev-parse", "--verify", "-q", "--short", "HEAD")
	if err != nil {
		repo.Unborn = true
		repo.Detached = false
	}
	repo.Head = head
	return repo, nil
}

// maxRepoDepth bounds how far ResolveRepos descends into an umbrella folder
// looking for nested repos.
const maxRepoDepth = 3

// skipDirs are directories ResolveRepos never descends into while hunting for
// nested repos: dependency trees, build output, and agent-manager's own
// worktree scratch dirs would only add noise or duplicate a repo already found.
var skipDirs = map[string]bool{
	"node_modules":    true,
	".worktrees":      true,
	".claude":         true,
	".archive":        true,
	"dist":            true,
	"build":           true,
	"vendor":          true,
	".playwright-mcp": true,
}

// ResolveRepos returns the git repo roots reachable from cwd, most-active
// first. When cwd is itself a repo, that repo plus its linked worktrees are
// the roots. When cwd is an
// umbrella folder holding several repos, each nested repo root is returned
// ranked with dirty working trees before clean ones, then by most recent
// commit, so review lands on the repo the agent is most likely editing.
func (d *Driver) ResolveRepos(cwd string) ([]string, error) {
	root, err := d.toplevel(cwd)
	if err == nil {
		return d.expandWorktrees([]string{root}), nil
	}
	if !errors.Is(err, ErrNotARepo) {
		return nil, err
	}
	roots := d.discoverRepos(cwd)
	if len(roots) == 0 {
		return nil, ErrNotARepo
	}
	roots = d.expandWorktrees(roots)
	d.rankRepos(roots)
	return roots, nil
}

// expandWorktrees adds each discovered repo's linked worktrees to the
// candidates, so a worktree living outside the umbrella still ranks.
func (d *Driver) expandWorktrees(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	identity := func(path string) string {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return resolved
		}
		return path
	}
	var expanded []string
	add := func(path string) {
		key := identity(path)
		if seen[key] {
			return
		}
		seen[key] = true
		expanded = append(expanded, path)
	}
	for _, root := range roots {
		add(root)
		worktrees, err := d.Worktrees(root)
		if err != nil {
			continue
		}
		for _, worktree := range worktrees {
			add(worktree.Root)
		}
	}
	return expanded
}

func (d *Driver) discoverRepos(dir string) []string {
	var roots []string
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		entries, err := os.ReadDir(path)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.Name() == ".git" {
				roots = append(roots, path)
				return
			}
		}
		if depth >= maxRepoDepth {
			return
		}
		for _, entry := range entries {
			if !entry.IsDir() || skipDirs[entry.Name()] {
				continue
			}
			walk(filepath.Join(path, entry.Name()), depth+1)
		}
	}
	walk(dir, 0)
	return roots
}

// maxDirtyProbes caps the expensive status calls: recency ranks everything
// cheaply first, then only the freshest few are probed for a dirty tree.
const maxDirtyProbes = 8

func (d *Driver) rankRepos(roots []string) {
	when := make([]int64, len(roots))
	forEachIndex(len(roots), func(i int) {
		if out, err := d.run(roots[i], "log", "-1", "--format=%ct"); err == nil {
			when[i], _ = strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		}
	})
	byWhen := make(map[string]int64, len(roots))
	for i, root := range roots {
		byWhen[root] = when[i]
	}
	sort.SliceStable(roots, func(i, j int) bool {
		return byWhen[roots[i]] > byWhen[roots[j]]
	})
	probes := len(roots)
	if probes > maxDirtyProbes {
		probes = maxDirtyProbes
	}
	dirty := make([]bool, probes)
	forEachIndex(probes, func(i int) {
		if out, err := d.run(roots[i], "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
			dirty[i] = true
		}
	})
	byDirty := make(map[string]bool, probes)
	for i, root := range roots[:probes] {
		byDirty[root] = dirty[i]
	}
	sort.SliceStable(roots[:probes], func(i, j int) bool {
		return byDirty[roots[i]] && !byDirty[roots[j]]
	})
}

// forEachIndex fans work across a bounded worker pool; each git call spawns
// a process, so serial ranking of dozens of worktrees pays seconds in spawns.
func forEachIndex(count int, work func(i int)) {
	workers := 8
	if count < workers {
		workers = count
	}
	if workers <= 1 {
		for i := 0; i < count; i++ {
			work(i)
		}
		return
	}
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				work(i)
			}
		}()
	}
	for i := 0; i < count; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
}

// BaseRef finds the merge base against the repo's default base for the
// branch scope, returning the resolved ref and a short description.
func (d *Driver) BaseRef(root string) (ref, describe string, err error) {
	candidate := d.DefaultBase(root)
	if candidate == "" {
		return "", "", errors.New("no default branch found on upstream, origin, or as a local main or master")
	}
	return d.baseRefFor(root, candidate)
}

// DefaultBase is the branch work in the repo merges into: the default
// branch on upstream, which GitHub's fork layout names the parent, else on
// origin, else the local branch. "" when the repo names no default branch.
func (d *Driver) DefaultBase(dir string) string {
	name := d.defaultBranchName(dir)
	if name == "" {
		return ""
	}
	for _, remoteRef := range []string{"upstream/" + name, "origin/" + name} {
		if d.ResolveRef(dir, "refs/remotes/"+remoteRef) == nil {
			return remoteRef
		}
	}
	if d.ResolveRef(dir, "refs/heads/"+name) == nil {
		return name
	}
	return ""
}

// defaultBranchName reads the default branch a clone or fetch recorded as
// a remote's HEAD, or takes a local main or master when no remote names one.
func (d *Driver) defaultBranchName(dir string) string {
	for _, remote := range []string{"upstream", "origin"} {
		if target, err := d.run(dir, "symbolic-ref", "-q", "--short", "refs/remotes/"+remote+"/HEAD"); err == nil {
			return strings.TrimPrefix(target, remote+"/")
		}
	}
	for _, name := range []string{"main", "master"} {
		if d.ResolveRef(dir, "refs/heads/"+name) == nil {
			return name
		}
	}
	return ""
}

// baseRefFor returns the merge base of candidate against HEAD and a short
// "<candidate>@<short>" description.
func (d *Driver) baseRefFor(root, candidate string) (ref, describe string, err error) {
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

// BranchBase resolves the base ref the branch scope diffs against. A non-empty
// override is validated first and fails loudly when it no longer resolves,
// never falling back to auto-detection; empty override auto-detects.
func (d *Driver) BranchBase(root, override string) (ref, describe string, err error) {
	if override != "" {
		if err := d.ResolveRef(root, override); err != nil {
			return "", "", err
		}
		return d.baseRefFor(root, override)
	}
	return d.BaseRef(root)
}

// BranchRefs lists local and remote branch short names, dropping the
// symbolic refs such as origin/HEAD that name a remote's default rather
// than a branch.
func (d *Driver) BranchRefs(root string) ([]string, error) {
	out, err := d.run(root, "for-each-ref", "--format=%(symref) %(refname:short)", "refs/heads", "refs/remotes")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(out, "\n") {
		symref, name, _ := strings.Cut(line, " ")
		if symref != "" || name == "" {
			continue
		}
		refs = append(refs, name)
	}
	return refs, nil
}

func (d *Driver) ResolveRef(root, ref string) error {
	if _, err := d.run(root, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
		return fmt.Errorf("ref %q does not resolve to a commit", ref)
	}
	return nil
}

type Status = value.Status

const (
	Added     = value.Added
	Modified  = value.Modified
	Deleted   = value.Deleted
	Renamed   = value.Renamed
	Copied    = value.Copied
	Untracked = value.Untracked
	Unmerged  = value.Unmerged
)

type ChangedFile = value.ChangedFile

// LastCommitParent returns HEAD's parent ref, or git's empty-tree object
// when HEAD is a root commit.
func (d *Driver) LastCommitParent(root string) string {
	if _, err := d.run(root, "rev-parse", "--verify", "-q", "HEAD~1"); err != nil {
		return emptyTree
	}
	return "HEAD~1"
}

// diffRange returns the base ref and diff arguments for a scope; baseRef
// is only consulted for ScopeBranch.
func (d *Driver) diffRange(root string, scope Scope, baseRef string) (base string, args []string, err error) {
	switch scope {
	case ScopeStaged:
		return "", []string{"--cached"}, nil
	case ScopeLastCommit:
		parent := d.LastCommitParent(root)
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
			// a nested repository is listed as a directory and belongs to its own review
			if strings.HasSuffix(path, "/") {
				continue
			}
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

type FileStat = value.FileStat

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

func (d *Driver) IndexFile(root, path string) ([]byte, error) {
	return d.ShowFile(root, ":0", path)
}

// maxCountBytes bounds the scan CountWorkingLines is willing to do.
const maxCountBytes = 8 << 20

// LineCount reports a file's lines; Counted is false when it could not be scanned.
type LineCount struct {
	Lines   int
	Binary  bool
	Counted bool
}

// CountWorkingLines streams a working-tree file, so files too big to diff still count.
func (d *Driver) CountWorkingLines(root, path string) (LineCount, error) {
	file, err := os.Open(filepath.Join(root, path))
	if errors.Is(err, os.ErrNotExist) {
		return LineCount{}, nil
	}
	if err != nil {
		return LineCount{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return LineCount{}, err
	}
	if info.Size() > maxCountBytes {
		return LineCount{}, nil
	}

	buf := make([]byte, 32*1024)
	count := LineCount{Counted: true}
	total := 0
	var lastByte byte
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			if total == 0 && IsBinary(chunk) {
				return LineCount{Binary: true, Counted: true}, nil
			}
			total += n
			count.Lines += bytes.Count(chunk, []byte{'\n'})
			lastByte = chunk[n-1]
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return LineCount{}, readErr
		}
	}
	if total > 0 && lastByte != '\n' {
		count.Lines++
	}
	return count, nil
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

func (d *Driver) IsRepoRoot(dir string) bool {
	top, err := d.toplevel(dir)
	if err != nil {
		return false
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	resolvedTop, err := filepath.EvalSymlinks(top)
	if err != nil {
		return false
	}
	return resolvedDir == resolvedTop
}
