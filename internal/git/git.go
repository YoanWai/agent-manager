// Package git shells out to the git CLI to read diff data for a repo.
package git

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
	return d.runInput(dir, "", args...)
}

func (d *Driver) runInput(dir, input string, args ...string) (string, error) {
	cmd := exec.Command(d.bin, append([]string{"-c", "core.quotepath=false"}, args...)...)
	cmd.Dir = dir
	// The manager's UI owns the terminal, so a credential, askpass or ssh
	// prompt from a hook or LFS filter would hang unseen; off its terminal
	// and with prompts disabled, git fails instead.
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "SSH_ASKPASS_REQUIRE=never")
	detachTerminal(cmd)
	if input != "" {
		cmd.Stdin = strings.NewReader(input)
	}
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
	ScopeCommit      = value.ScopeCommit
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
	candidate, err := d.DefaultBase(root)
	if err != nil {
		return "", "", err
	}
	if candidate == "" {
		return "", "", errors.New("no default branch found on a remote, or as a local main or master")
	}
	return d.baseRefFor(root, candidate)
}

// DefaultBase is the branch work in the repo merges into: the default
// branch on upstream, which GitHub's fork layout names the parent, else on
// origin, else on any other remote, else the local branch. "" when the repo
// names no default branch.
func (d *Driver) DefaultBase(dir string) (string, error) {
	remotes, err := d.baseRemotes(dir)
	if err != nil {
		return "", err
	}
	name := d.defaultBranchName(dir, remotes)
	if name == "" {
		return "", nil
	}
	candidates := make([]string, 0, len(remotes)+1)
	for _, remote := range remotes {
		candidates = append(candidates, "refs/remotes/"+remote+"/"+name)
	}
	for _, full := range append(candidates, "refs/heads/"+name) {
		if d.ResolveRef(dir, full) == nil {
			return d.shortRef(dir, full)
		}
	}
	return "", nil
}

// shortRef names full the shortest way git resolves back to it, so a local
// branch or tag sharing the short name cannot stand in for it.
func (d *Driver) shortRef(dir, full string) (string, error) {
	return d.run(dir, "rev-parse", "--abbrev-ref", full)
}

// baseRemotes lists the repo's remotes in the order a base is looked for on
// them: upstream, then origin, then the rest by name.
func (d *Driver) baseRemotes(dir string) ([]string, error) {
	out, err := d.run(dir, "remote")
	if err != nil {
		return nil, err
	}
	rank := func(name string) int {
		switch name {
		case "upstream":
			return 0
		case "origin":
			return 1
		}
		return 2
	}
	remotes := strings.Fields(out)
	slices.SortFunc(remotes, func(a, b string) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a, b))
	})
	return remotes, nil
}

// defaultBranchName reads the default branch a clone or fetch recorded as
// a remote's HEAD, or takes a local main or master when no remote names one.
// A HEAD naming a branch its remote no longer has, as a removed or renamed
// remote leaves behind, names nothing.
func (d *Driver) defaultBranchName(dir string, remotes []string) string {
	for _, remote := range remotes {
		// The full target, since --short keeps it whole when a local branch
		// shares the short name.
		target, err := d.run(dir, "symbolic-ref", "-q", "refs/remotes/"+remote+"/HEAD")
		if err == nil && d.ResolveRef(dir, target) == nil {
			return strings.TrimPrefix(target, "refs/remotes/"+remote+"/")
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

type Commit = value.Commit

// maxCommits caps the commit list; the picker filters past what it shows.
const maxCommits = 500

// Commits lists the commits the commit scope steps through, newest first:
// those since the merge base with the target, or HEAD's recent history when
// the branch has none of its own, which history reports. describe names the
// target the way BranchBase does, "" when none resolves. An override that no
// longer resolves fails the way the branch scope does; an unborn HEAD has no
// commits.
func (d *Driver) Commits(root, override string) (commits []Commit, describe string, history bool, err error) {
	if _, err := d.run(root, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		if unbornHead(err) {
			return nil, "", false, nil
		}
		return nil, "", false, err
	}
	base, describe, err := d.BranchBase(root, override)
	if err != nil && override != "" {
		return nil, "", false, err
	}
	if err == nil {
		if commits, err = d.log(root, base+"..HEAD"); err != nil {
			return nil, "", false, err
		}
	}
	if len(commits) == 0 {
		commits, err = d.log(root, "HEAD")
		history = true
	}
	return commits, describe, history, err
}

// commitHeader opens each commit's record in log's output; the paths that
// follow can hold anything but a NUL, but never a full SHA and a unit
// separator.
var commitHeader = regexp.MustCompile(`^[0-9a-f]{40,64}\x1f`)

// unbornHead reports whether a `rev-parse --verify -q HEAD` failure means
// only that HEAD names no commit yet, which -q reports as a quiet exit 1.
func unbornHead(err error) bool {
	var exit *exec.ExitError
	return errors.As(err, &exit) && exit.ExitCode() == 1
}

// log lists each commit's paths the way the commit scope diffs it: against
// its first parent with renames found, so a merge lists what it brings in and
// a rename one path, whatever diff.renames says. A signature check that
// log.showSignature asks for would land ahead of the headers.
func (d *Driver) log(root string, revs ...string) ([]Commit, error) {
	args := append([]string{"log", "-z", "--no-show-signature", "--name-only", "-M", "--diff-merges=first-parent", "--max-count=" + strconv.Itoa(maxCommits), "--format=%H%x1f%h%x1f%s"}, revs...)
	out, err := d.run(root, args...)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, field := range splitNUL(out) {
		if commitHeader.MatchString(field) {
			parts := strings.SplitN(field, "\x1f", 3)
			commits = append(commits, Commit{SHA: parts[0], Short: parts[1], Subject: parts[2]})
			continue
		}
		// The first path follows its header after a newline.
		if path := strings.TrimPrefix(field, "\n"); path != "" {
			if len(commits) == 0 {
				return nil, fmt.Errorf("git log: unexpected output before the first commit: %q", field)
			}
			last := &commits[len(commits)-1]
			last.Files = append(last.Files, path)
		}
	}
	return commits, nil
}

// CommitSHA resolves rev to the full SHA of the commit it names.
func (d *Driver) CommitSHA(root, rev string) (string, error) {
	sha, err := d.run(root, "rev-parse", "--verify", "-q", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("%q does not resolve to a commit", rev)
	}
	return sha, nil
}

// Rewrites maps each of olds that HEAD no longer reaches to the commit in
// commits that took its place after an amend or rebase: the one carrying
// the same patch, else the only one left that can have replaced it under the
// same subject. A commit matching neither, one a local branch still holds,
// as after a checkout of a sibling branch, or one whose object git has
// pruned, is left out.
func (d *Driver) Rewrites(root string, olds []string, commits []Commit) (map[string]string, error) {
	var gone []string
	for _, old := range olds {
		if _, err := d.run(root, "merge-base", "--is-ancestor", old, "HEAD"); err == nil {
			continue
		}
		if _, err := d.run(root, "cat-file", "-e", old+"^{commit}"); err != nil {
			continue
		}
		if branch, err := d.run(root, "for-each-ref", "--count=1", "--contains", old, "refs/heads"); err != nil {
			return nil, err
		} else if branch != "" {
			continue
		}
		gone = append(gone, old)
	}
	if len(gone) == 0 || len(commits) == 0 {
		return nil, nil
	}
	revs := append([]string(nil), gone...)
	for _, c := range commits {
		revs = append(revs, c.SHA)
	}
	patches, err := d.patchIDs(root, revs)
	if err != nil {
		return nil, err
	}
	previous, err := d.log(root, append([]string{"--no-walk=unsorted"}, gone...)...)
	if err != nil {
		return nil, err
	}
	byPatch := map[string]string{}
	for _, c := range commits {
		if patch := patches[c.SHA]; patch != "" {
			if _, seen := byPatch[patch]; !seen {
				byPatch[patch] = c.SHA
			}
		}
	}
	moved := map[string]string{}
	claimed := map[string]bool{}
	for _, old := range olds {
		claimed[old] = true
	}
	var unmatched []Commit
	for _, old := range previous {
		if sha, ok := byPatch[patches[old.SHA]]; ok {
			moved[old.SHA] = sha
			claimed[sha] = true
		} else {
			unmatched = append(unmatched, old)
		}
	}
	for _, old := range unmatched {
		if sha := d.subjectReplacement(root, old, commits, claimed); sha != "" {
			moved[old.SHA] = sha
		}
	}
	return moved, nil
}

// subjectReplacement is the one commit that can have replaced old by subject
// alone: same subject, no review state or patch match of its own, and not
// one of old's ancestors, which predate it. "" when none or several can.
func (d *Driver) subjectReplacement(root string, old Commit, commits []Commit, claimed map[string]bool) string {
	found := ""
	for _, c := range commits {
		if c.Subject != old.Subject || claimed[c.SHA] {
			continue
		}
		if _, err := d.run(root, "merge-base", "--is-ancestor", c.SHA, old.SHA); err == nil {
			continue
		}
		if found != "" {
			return ""
		}
		found = c.SHA
	}
	return found
}

// patchIDs returns the stable patch id of each rev; a commit with an empty
// diff has none.
func (d *Driver) patchIDs(root string, revs []string) (map[string]string, error) {
	patches, err := d.run(root, append([]string{"show", "--no-color", "--no-ext-diff", "--no-show-signature", "--format=commit %H"}, revs...)...)
	if err != nil {
		return nil, err
	}
	out, err := d.runInput(root, patches+"\n", "patch-id", "--stable")
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if patch, sha, ok := strings.Cut(line, " "); ok {
			ids[sha] = patch
		}
	}
	return ids, nil
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

// CommitParent returns rev's first parent, or git's empty-tree object when
// rev is a root commit.
func (d *Driver) CommitParent(root, rev string) string {
	if _, err := d.run(root, "rev-parse", "--verify", "-q", rev+"~1"); err != nil {
		return emptyTree
	}
	return rev + "~1"
}

// diffRange returns the base ref and diff arguments for a scope. ref is the
// merge base for ScopeBranch and the commit for ScopeCommit, where "" means
// HEAD; other scopes ignore it.
func (d *Driver) diffRange(root string, scope Scope, ref string) (base string, args []string, err error) {
	switch scope {
	case ScopeStaged:
		return "", []string{"--cached"}, nil
	case ScopeCommit:
		rev := cmp.Or(ref, "HEAD")
		parent := d.CommitParent(root, rev)
		return parent, []string{parent, rev}, nil
	case ScopeBranch:
		return ref, []string{ref, "HEAD"}, nil
	default:
		return "HEAD", []string{"HEAD"}, nil
	}
}

func (d *Driver) ChangedFiles(root string, scope Scope, ref string) ([]ChangedFile, error) {
	_, rangeArgs, err := d.diffRange(root, scope, ref)
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

func (d *Driver) NumStat(root string, scope Scope, ref string) (map[string]FileStat, error) {
	_, rangeArgs, err := d.diffRange(root, scope, ref)
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
func (d *Driver) Fingerprint(root string, scope Scope, ref string) (uint64, error) {
	head, _ := d.run(root, "rev-parse", "-q", "--verify", "HEAD")
	porcelain, err := d.run(root, "status", "--porcelain", "-z")
	if err != nil {
		return 0, err
	}
	hash := fnv.New64a()
	fmt.Fprintf(hash, "%d|%s|%s|%s", scope, ref, head, porcelain)
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
