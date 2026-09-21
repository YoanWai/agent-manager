package federation

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
)

type Review struct {
	Set         diff.Set
	Roots       []string
	Fingerprint uint64
	Worktrees   []git.Worktree
	File        *diff.FileDiff
}

// BuildReview runs on the checkout's host. It never writes repository state.
func BuildReview(directory string, scope git.Scope, base, repoWant, file string) (Review, error) {
	if scope < git.ScopeUncommitted || scope > git.ScopeStaged {
		return Review{}, fmt.Errorf("invalid review scope %d", scope)
	}
	driver, err := git.New()
	if err != nil {
		return Review{}, err
	}
	roots, err := driver.ResolveRepos(directory)
	if err != nil {
		return Review{}, err
	}
	root := roots[0]
	if repoWant != "" {
		found := false
		for _, candidate := range roots {
			if candidate == repoWant {
				root, found = candidate, true
				break
			}
		}
		if !found {
			return Review{}, fmt.Errorf("review repository %q is no longer among session repositories", repoWant)
		}
	}
	set, err := diff.BuildSet(driver, root, scope, base)
	if err != nil {
		return Review{}, err
	}
	result := Review{Set: set, Roots: roots}
	result.Fingerprint, err = driver.Fingerprint(root, scope, set.BaseRef)
	if err != nil {
		return Review{}, err
	}
	result.Worktrees, err = driver.Worktrees(root)
	if err != nil {
		return Review{}, err
	}
	if file != "" {
		for i := range set.Files {
			if set.Files[i].File.Path == file {
				loaded := diff.LoadFile(driver, set, i)
				result.File = &loaded
				return result, nil
			}
		}
		return Review{}, fmt.Errorf("review file %q is no longer changed", file)
	}
	return result, nil
}

func (c *Client) Review(ctx context.Context, host, directory string, scope git.Scope, base, repoWant, file string) (Review, error) {
	h, err := c.host(host)
	if err != nil {
		return Review{}, err
	}
	if h.SSH == "" {
		return BuildReview(directory, scope, base, repoWant, file)
	}
	if h.ReviewBinary != "" {
		h.Binary = h.ReviewBinary
	}
	b, err := c.rpc(ctx, h, "review-data", "--directory", directory, "--scope", strconv.Itoa(int(scope)), "--base", base, "--repo", repoWant, "--file", file)
	if err != nil {
		return Review{}, fmt.Errorf("review on %s: %w", host, err)
	}
	var review Review
	if err := json.Unmarshal(b, &review); err != nil {
		return Review{}, fmt.Errorf("review on %s: invalid response: %w", host, err)
	}
	return review, nil
}
