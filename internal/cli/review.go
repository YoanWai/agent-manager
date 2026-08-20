package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

const (
	usageRename        = `rename "<name>"`
	usageReviewRepo    = "review-repo <path>"
	usageReviewBase    = "review-base <ref>|--clear"
	usageReviewMode    = "review-mode <uncommitted|branch|last_commit|staged>"
	usageReviewComment = "review-comment <comment-id> [--reopen]"
)

func reviewSection() section {
	return section{
		title: "Your own session",
		commands: []command{
			{name: "rename", usage: usageRename, about: "name this session for the broad feature it is about, once, while it still carries a placeholder name", run: configCommand(runRename)},
			{name: "review-repo", usage: usageReviewRepo, about: "declare the repo or worktree you are working in, so the user's review screen opens on it", run: configCommand(runReviewRepo)},
			{name: "review-base", usage: usageReviewBase, about: "declare the ref your branch merges into, which review diffs against; --clear returns to auto-detection", run: configCommand(runReviewBase)},
			{name: "review-mode", usage: usageReviewMode, about: "point the user's review screen at the diff scope you want them to see", run: configCommand(runReviewMode)},
			{name: "review-comment", usage: usageReviewComment, about: "mark a review comment handled after addressing it; --reopen marks it open again", run: configCommand(runReviewComment)},
		},
	}
}

func runRename(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageRename)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	name, err := nonBlank(usageRename, operands[0])
	if err != nil {
		return err
	}
	message, err := sessioncmd.Rename(configDir, sessionID, name)
	return printMessage(out, message, err)
}

func runReviewRepo(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageReviewRepo)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	path, err := nonBlank(usageReviewRepo, operands[0])
	if err != nil {
		return err
	}
	message, err := sessioncmd.ReviewRepo(configDir, sessionID, path)
	return printMessage(out, message, err)
}

// The ref resolves in the repo holding the working directory the agent runs
// this from, which is how it names its own worktree without a flag.
func runReviewBase(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageReviewBase)
	clear := set.Bool("clear", false, "drop the declared ref and return to auto-detection")
	operands, err := parseCommand(out, set, args, 0, 1)
	if err != nil {
		return err
	}
	if (len(operands) == 1) == *clear {
		return usageError(usageReviewBase)
	}
	ref := ""
	if !*clear {
		if ref, err = nonBlank(usageReviewBase, operands[0]); err != nil {
			return err
		}
	}
	message, err := sessioncmd.ReviewBase(configDir, sessionID, ".", ref)
	return printMessage(out, message, err)
}

func runReviewMode(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageReviewMode)
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	message, err := sessioncmd.ReviewScope(configDir, sessionID, operands[0])
	return printMessage(out, message, err)
}

func runReviewComment(out io.Writer, args []string, sessionID, configDir string) error {
	set := newFlagSet(usageReviewComment)
	reopen := set.Bool("reopen", false, "mark the comment open again")
	operands, err := parseCommand(out, set, args, 1, 1)
	if err != nil {
		return err
	}
	message, err := sessioncmd.ReviewComment(configDir, sessionID, operands[0], !*reopen)
	return printMessage(out, message, err)
}

// A blank operand is a mis-quoted argument rather than a value, so it reads
// as the usage error it is instead of clearing what it meant to set.
func nonBlank(usage, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", usageError(usage)
	}
	return value, nil
}

func printMessage(out io.Writer, message string, err error) error {
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, message)
	return err
}
