// Package diff builds line models for changed files, using full-file
// context for small files and changed hunks for large ones.
package diff

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/git"
)

type LineKind = model.LineKind

const (
	Same = model.Same
	Add  = model.Add
	Del  = model.Del
	Gap  = model.Gap
)

type Span = model.Span
type Line = model.Line
type FileDiff = model.FileDiff
type Set = model.Set
type Row = model.Row

const maxDiffBytes = 32 << 20

// BuildSet loads only the changed-file metadata for a scope. File contents and
// line models are loaded on demand so large reviews can show their file list
// without waiting for every file to be read and diffed. A non-empty
// baseOverride selects the ScopeBranch base and fails loudly when it no longer
// resolves; empty keeps auto-detection.
func BuildSet(driver *git.Driver, cwd string, scope git.Scope, baseOverride string) (Set, error) {
	repo, err := driver.OpenRepo(cwd)
	if err != nil {
		return Set{}, err
	}
	set := Set{Repo: repo, Scope: scope, BaseOverride: baseOverride}

	baseRef := ""
	switch scope {
	case git.ScopeBranch:
		var describe string
		baseRef, describe, err = driver.BranchBase(repo.Root, baseOverride)
		if err != nil {
			return Set{}, err
		}
		set.BaseDesc = describe
		set.BaseRef = baseRef
	case git.ScopeStaged, git.ScopeUncommitted:
		set.BaseDesc = "HEAD"
	case git.ScopeLastCommit:
		set.BaseDesc = "HEAD~1"
	}
	if repo.Unborn && scope != git.ScopeUncommitted && scope != git.ScopeStaged {
		return set, nil
	}

	var files []git.ChangedFile
	var stats map[string]git.FileStat
	var filesErr, statsErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		files, filesErr = driver.ChangedFiles(repo.Root, scope, baseRef)
	}()
	go func() {
		defer wg.Done()
		stats, statsErr = driver.NumStat(repo.Root, scope, baseRef)
	}()
	wg.Wait()
	if filesErr != nil {
		return Set{}, filesErr
	}
	if statsErr != nil {
		return Set{}, statsErr
	}
	files = filterUntrackedSpecialFiles(repo.Root, files)

	for _, file := range files {
		stat, known := stats[file.Path]
		fd := FileDiff{File: file, Stat: stat, HasStat: known}
		set.Files = append(set.Files, fd)
	}
	fillUnknownStats(driver, repo.Root, set.Files)
	return set, nil
}

func filterUntrackedSpecialFiles(root string, files []git.ChangedFile) []git.ChangedFile {
	kept := files[:0]
	for _, file := range files {
		if file.Status == git.Untracked {
			info, err := os.Lstat(filepath.Join(root, file.Path))
			if err == nil && !info.Mode().IsRegular() {
				continue
			}
		}
		kept = append(kept, file)
	}
	return kept
}

// Git's numstat omits untracked files; count them before their first visit so
// the file list can still show +N or binary.
func fillUnknownStats(driver *git.Driver, root string, files []FileDiff) {
	var unknown []int
	for i := range files {
		if !files[i].HasStat && files[i].File.Status == git.Untracked {
			unknown = append(unknown, i)
		}
	}
	if len(unknown) == 0 {
		return
	}

	const maxWorkers = 8
	workers := min(len(unknown), maxWorkers)
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := countUnknownStat(driver, root, &files[i]); err != nil {
					files[i].Err = err
				}
			}
		}()
	}
	for _, i := range unknown {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

// LoadFile builds one file's line model without mutating set, which makes it
// safe to call from an asynchronous UI command using a snapshot of the set.
func LoadFile(driver *git.Driver, set Set, index int) FileDiff {
	if index < 0 || index >= len(set.Files) {
		return FileDiff{}
	}
	fd := set.Files[index]
	if !fd.IsLoaded {
		loadFile(driver, set.Repo.Root, set.Scope, set.BaseRef, &fd)
	}
	return fd
}

func loadFile(driver *git.Driver, root string, scope git.Scope, baseRef string, fd *FileDiff) {
	fd.IsLoaded = true
	if !fd.HasStat && fd.File.Status == git.Untracked {
		if err := countUnknownStat(driver, root, fd); err != nil {
			fd.Err = err
			return
		}
	}
	oldContent, newContent, err := fileSides(driver, root, scope, baseRef, fd.File)
	if err != nil {
		fd.Err = err
		return
	}
	if git.IsBinary(oldContent) || git.IsBinary(newContent) {
		fd.Binary = true
		return
	}
	if len(oldContent) > maxDiffBytes || len(newContent) > maxDiffBytes {
		fd.Truncated = true
		return
	}
	known := fd.HasStat
	*fd = BuildFile(oldContent, newContent, fd.File, fd.Stat)
	fd.HasStat = known
	fd.IsLoaded = true
}

// Counting raw bytes keeps untracked files correct past the diff model's caps.
func countUnknownStat(driver *git.Driver, root string, fd *FileDiff) error {
	count, err := driver.CountWorkingLines(root, fd.File.Path)
	if err != nil {
		return err
	}
	if !count.Counted {
		return nil
	}
	fd.HasStat = true
	fd.Stat = git.FileStat{Adds: count.Lines, Binary: count.Binary}
	fd.Binary = count.Binary
	return nil
}

func fileSides(driver *git.Driver, root string, scope git.Scope, baseRef string, file git.ChangedFile) (oldContent, newContent []byte, err error) {
	oldRef, newRef := "", ""
	switch scope {
	case git.ScopeUncommitted:
		oldRef = "HEAD"
	case git.ScopeStaged:
		oldRef, newRef = "HEAD", ":0"
	case git.ScopeLastCommit:
		oldRef, newRef = baseRef, "HEAD"
		if oldRef == "" {
			oldRef = driver.LastCommitParent(root)
		}
	case git.ScopeBranch:
		oldRef, newRef = baseRef, "HEAD"
	}

	if file.Status != git.Added && file.Status != git.Untracked {
		oldContent, err = driver.ShowFile(root, oldRef, file.OldPath)
		if err != nil {
			return nil, nil, err
		}
	}
	if file.Status != git.Deleted {
		if newRef == "" {
			newContent, err = driver.WorkingFile(root, file.Path)
		} else {
			newContent, err = driver.ShowFile(root, newRef, file.Path)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	return oldContent, newContent, nil
}

// BuildFile interleaves deletions with new-file lines. Large files show
// only changed hunks, with gap markers for omitted context.
func BuildFile(oldContent, newContent []byte, file git.ChangedFile, stat git.FileStat) FileDiff {
	return model.BuildFile(oldContent, newContent, file, stat)
}
