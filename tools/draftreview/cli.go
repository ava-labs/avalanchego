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
}

func (createCommand) isCommand() {}

type deleteCommand struct {
	Repo      string
	PRNumber  int
	ConfigDir string
}

func (deleteCommand) isCommand() {}

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
	}, nil
}

func parseDeleteCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")

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
	}, nil
}

func Usage() string {
	return `gh-draft-review creates and manages pending GitHub pull request reviews.

Usage:
  gh-draft-review create --pr NUMBER [--repo OWNER/REPO] --body TEXT [--config-dir DIR]
  gh-draft-review delete --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR]
  gh-draft-review version

Notes:
  - This tool only manipulates pending reviews owned by the authenticated user.
  - It uses isolated gh auth from --config-dir.
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
