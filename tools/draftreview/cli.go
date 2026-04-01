package draftreview

import (
	"errors"
	"flag"
	"fmt"
	"io"
)

type command interface {
	isCommand()
}

type createCommand struct {
	Repo      string
	PRNumber  int
	Body      string
	ConfigDir string
	StateDir  string
}

func (createCommand) isCommand() {}

type deleteCommand struct {
	Repo      string
	PRNumber  int
	ConfigDir string
	StateDir  string
}

func (deleteCommand) isCommand() {}

type getCommand struct {
	Repo      string
	PRNumber  int
	ConfigDir string
	StateDir  string
}

func (getCommand) isCommand() {}

type updateBodyCommand struct {
	Repo      string
	PRNumber  int
	Body      string
	ConfigDir string
	StateDir  string
	Force     bool
}

func (updateBodyCommand) isCommand() {}

type replaceCommentsCommand struct {
	Repo         string
	PRNumber     int
	CommentsFile string
	ConfigDir    string
	StateDir     string
	Force        bool
}

func (replaceCommentsCommand) isCommand() {}

type versionCommand struct{}

func (versionCommand) isCommand() {}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usageError("missing command")
	}

	switch args[0] {
	case "-h", "--help", "help":
		return nil, usageError("")
	case "--version", "version":
		return versionCommand{}, nil
	case "create":
		return parseCreateCommand(args[1:])
	case "delete":
		return parseDeleteCommand(args[1:])
	case "get":
		return parseGetCommand(args[1:])
	case "replace-comments":
		return parseReplaceCommentsCommand(args[1:])
	case "update-body":
		return parseUpdateBodyCommand(args[1:])
	default:
		return nil, usageError(fmt.Sprintf("unknown command %q", args[0]))
	}
}

func parseCreateCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	body := flags.String("body", "", "review body")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if *body == "" {
		return nil, usageError("--body is required")
	}

	return createCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		Body:      *body,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
	}, nil
}

func parseDeleteCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}

	return deleteCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
	}, nil
}

func parseGetCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}

	return getCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
	}, nil
}

func parseUpdateBodyCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("update-body", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	body := flags.String("body", "", "review body")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	force := flags.Bool("force", false, "overwrite even if the review body differs from stored state")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if *body == "" {
		return nil, usageError("--body is required")
	}

	return updateBodyCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		Body:      *body,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
		Force:     *force,
	}, nil
}

func parseReplaceCommentsCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("replace-comments", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	commentsFile := flags.String("comments-file", "", "path to a JSON file containing inline comments")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	force := flags.Bool("force", false, "overwrite even if the pending review comments differ from stored state")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if *commentsFile == "" {
		return nil, usageError("--comments-file is required")
	}

	return replaceCommentsCommand{
		Repo:         *repo,
		PRNumber:     *prNumber,
		CommentsFile: *commentsFile,
		ConfigDir:    *configDir,
		StateDir:     *stateDir,
		Force:        *force,
	}, nil
}

func Usage() string {
	return `gh-pending-review creates and manages pending GitHub pull request reviews.

Usage:
  gh-pending-review create --pr NUMBER [--repo OWNER/REPO] --body TEXT [--config-dir DIR] [--state-dir DIR]
  gh-pending-review delete --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR]
  gh-pending-review get --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR]
  gh-pending-review replace-comments --pr NUMBER [--repo OWNER/REPO] --comments-file PATH [--config-dir DIR] [--state-dir DIR] [--force]
  gh-pending-review update-body --pr NUMBER [--repo OWNER/REPO] --body TEXT [--config-dir DIR] [--state-dir DIR] [--force]
  gh-pending-review version

Notes:
  - This tool only manipulates pending reviews owned by the authenticated user.
  - It uses isolated gh auth from --config-dir.
  - It stores the last published review body and comment set locally to detect user edits before update.
  - It never submits a review.
`
}

type usageErr struct {
	message string
}

func (e usageErr) Error() string {
	if e.message == "" {
		return Usage()
	}
	return fmt.Sprintf("%s\n\n%s", e.message, Usage())
}

func usageError(message string) error {
	return usageErr{message: message}
}

func IsUsageError(err error) bool {
	var target usageErr
	return errors.As(err, &target)
}
