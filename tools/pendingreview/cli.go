// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
)

type command interface {
	isCommand()
	name() string
}

type createCommand struct {
	Repo      string
	PRNumber  int
	Body      string
	BodyFile  string
	ConfigDir string
	StateDir  string
	JSON      bool
}

func (createCommand) isCommand()   {}
func (createCommand) name() string { return "create" }

type deleteCommand struct {
	Repo         string
	PRNumber     int
	ConfigDir    string
	StateDir     string
	EnsureAbsent bool
	JSON         bool
}

func (deleteCommand) isCommand()   {}
func (deleteCommand) name() string { return "delete" }

type getCommand struct {
	Repo      string
	PRNumber  int
	ConfigDir string
	StateDir  string
	Pretty    bool
}

func (getCommand) isCommand()   {}
func (getCommand) name() string { return "get" }

type getStateCommand struct {
	Repo      string
	PRNumber  int
	UserLogin string
	StateDir  string
	Pretty    bool
}

func (getStateCommand) isCommand()   {}
func (getStateCommand) name() string { return "get-state" }

type updateBodyCommand struct {
	Repo      string
	PRNumber  int
	Body      string
	BodyFile  string
	ConfigDir string
	StateDir  string
	Force     bool
	JSON      bool
}

func (updateBodyCommand) isCommand()   {}
func (updateBodyCommand) name() string { return "update-body" }

type deleteStateCommand struct {
	Repo      string
	PRNumber  int
	UserLogin string
	StateDir  string
	JSON      bool
}

func (deleteStateCommand) isCommand()   {}
func (deleteStateCommand) name() string { return "delete-state" }

type replaceCommentsCommand struct {
	Repo            string
	PRNumber        int
	CommentsFile    string
	Comments        []DraftReviewEntry
	ConfigDir       string
	StateDir        string
	Force           bool
	CreateIfMissing bool
	ReviewBody      string
	ReviewBodyFile  string
	JSON            bool
}

func (replaceCommentsCommand) isCommand()   {}
func (replaceCommentsCommand) name() string { return "replace-comments" }

type upsertCommentCommand struct {
	Repo            string
	PRNumber        int
	CommentID       string
	Path            string
	Line            int
	Side            string
	StartLine       int
	StartSide       string
	Body            string
	BodyFile        string
	ConfigDir       string
	StateDir        string
	Force           bool
	CreateIfMissing bool
	ReviewBody      string
	ReviewBodyFile  string
	JSON            bool
}

func (upsertCommentCommand) isCommand()   {}
func (upsertCommentCommand) name() string { return "upsert-comment" }

type versionCommand struct{}

func (versionCommand) isCommand()   {}
func (versionCommand) name() string { return "version" }

type commandSpec struct {
	name        string
	parse       func(args []string) (command, error)
	decodeProxy func(payload json.RawMessage) (command, error)
}

var commandRegistry = []commandSpec{
	{name: "version", parse: func(_ []string) (command, error) { return versionCommand{}, nil }, decodeProxy: decodeProxyVersionCommand},
	{name: "create", parse: parseCreateCommand, decodeProxy: decodeProxyCreateCommand},
	{name: "delete", parse: parseDeleteCommand, decodeProxy: decodeProxyDeleteCommand},
	{name: "get", parse: parseGetCommand, decodeProxy: decodeProxyGetCommand},
	{name: "get-state", parse: parseGetStateCommand, decodeProxy: decodeProxyGetStateCommand},
	{name: "replace-comments", parse: parseReplaceCommentsCommand, decodeProxy: decodeProxyReplaceCommentsCommand},
	{name: "upsert-comment", parse: parseUpsertCommentCommand, decodeProxy: decodeProxyUpsertCommentCommand},
	{name: "delete-state", parse: parseDeleteStateCommand, decodeProxy: decodeProxyDeleteStateCommand},
	{name: "update-body", parse: parseUpdateBodyCommand, decodeProxy: decodeProxyUpdateBodyCommand},
}

func proxyCommandRegistry() []commandSpec {
	return commandRegistry
}

func lookupCommandSpec(name string) (commandSpec, bool) {
	for _, spec := range commandRegistry {
		if spec.name == name {
			return spec, true
		}
	}
	return commandSpec{}, false
}

func parseCommand(args []string) (command, error) {
	if len(args) == 0 {
		return nil, usageError("missing command")
	}

	switch args[0] {
	case "-h", "--help", "help":
		return nil, usageError("")
	case "--version":
		return versionCommand{}, nil
	default:
		spec, ok := lookupCommandSpec(args[0])
		if !ok {
			return nil, usageError(fmt.Sprintf("unknown command %q", args[0]))
		}
		return spec.parse(args[1:])
	}
}

func parseCreateCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("create", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	body := flags.String("body", "", "review body")
	bodyFile := flags.String("body-file", "", "path to a file containing the review body")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if (*body == "") == (*bodyFile == "") {
		return nil, usageError("exactly one of --body or --body-file is required")
	}

	return createCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		Body:      *body,
		BodyFile:  *bodyFile,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
		JSON:      *jsonOutput,
	}, nil
}

func parseDeleteCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("delete", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	ensureAbsent := flags.Bool("ensure-absent", false, "succeed if no pending review exists and clear stored state if present")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

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
		Repo:         *repo,
		PRNumber:     *prNumber,
		ConfigDir:    *configDir,
		StateDir:     *stateDir,
		EnsureAbsent: *ensureAbsent,
		JSON:         *jsonOutput,
	}, nil
}

func parseGetCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("get", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	pretty := flags.Bool("pretty", false, "pretty-print JSON output")

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
		Pretty:    *pretty,
	}, nil
}

func parseGetStateCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("get-state", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	userLogin := flags.String("user", "", "GitHub login associated with the stored review state")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	pretty := flags.Bool("pretty", false, "pretty-print JSON output")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if *userLogin == "" {
		return nil, usageError("--user is required")
	}

	return getStateCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		UserLogin: *userLogin,
		StateDir:  *stateDir,
		Pretty:    *pretty,
	}, nil
}

func parseUpdateBodyCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("update-body", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	body := flags.String("body", "", "review body")
	bodyFile := flags.String("body-file", "", "path to a file containing the review body")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	force := flags.Bool("force", false, "overwrite even if the review body differs from stored state")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if (*body == "") == (*bodyFile == "") {
		return nil, usageError("exactly one of --body or --body-file is required")
	}

	return updateBodyCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		Body:      *body,
		BodyFile:  *bodyFile,
		ConfigDir: *configDir,
		StateDir:  *stateDir,
		Force:     *force,
		JSON:      *jsonOutput,
	}, nil
}

func parseDeleteStateCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("delete-state", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	userLogin := flags.String("user", "", "GitHub login associated with the stored review state")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if *userLogin == "" {
		return nil, usageError("--user is required")
	}

	return deleteStateCommand{
		Repo:      *repo,
		PRNumber:  *prNumber,
		UserLogin: *userLogin,
		StateDir:  *stateDir,
		JSON:      *jsonOutput,
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
	createIfMissing := flags.Bool("create-if-missing", false, "create a pending review automatically when mutating comments and no draft exists")
	reviewBody := flags.String("review-body", "", "review body to use when creating a missing pending review")
	reviewBodyFile := flags.String("review-body-file", "", "path to a file containing the review body for a missing pending review")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

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
	if *reviewBody != "" && *reviewBodyFile != "" {
		return nil, usageError("at most one of --review-body or --review-body-file may be provided")
	}

	return replaceCommentsCommand{
		Repo:            *repo,
		PRNumber:        *prNumber,
		CommentsFile:    *commentsFile,
		ConfigDir:       *configDir,
		StateDir:        *stateDir,
		Force:           *force,
		CreateIfMissing: *createIfMissing,
		ReviewBody:      *reviewBody,
		ReviewBodyFile:  *reviewBodyFile,
		JSON:            *jsonOutput,
	}, nil
}

func parseUpsertCommentCommand(args []string) (command, error) {
	flags := flag.NewFlagSet("upsert-comment", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	repo := flags.String("repo", defaultRepo, "repository in OWNER/REPO form")
	prNumber := flags.Int("pr", 0, "pull request number")
	commentID := flags.String("comment-id", "", "existing draft review comment ID to update")
	path := flags.String("path", "", "path for a new-thread draft review comment")
	line := flags.Int("line", 0, "line for a new-thread draft review comment")
	side := flags.String("side", "", "side for a new-thread draft review comment")
	startLine := flags.Int("start-line", 0, "start line for a multi-line new-thread draft review comment")
	startSide := flags.String("start-side", "", "start side for a multi-line new-thread draft review comment")
	body := flags.String("body", "", "comment body")
	bodyFile := flags.String("body-file", "", "path to a file containing the comment body")
	configDir := flags.String("config-dir", defaultConfigDir(), "isolated gh config directory")
	stateDir := flags.String("state-dir", defaultStateDir(), "local draft review state directory")
	force := flags.Bool("force", false, "overwrite even if unrelated pending review comments differ from stored state")
	createIfMissing := flags.Bool("create-if-missing", false, "create a pending review automatically when mutating comments and no draft exists")
	reviewBody := flags.String("review-body", "", "review body to use when creating a missing pending review")
	reviewBodyFile := flags.String("review-body-file", "", "path to a file containing the review body for a missing pending review")
	jsonOutput := flags.Bool("json", false, "print machine-readable JSON success output")

	if err := flags.Parse(args); err != nil {
		return nil, usageError(err.Error())
	}
	if flags.NArg() != 0 {
		return nil, usageError(fmt.Sprintf("unexpected trailing arguments: %v", flags.Args()))
	}
	if *prNumber <= 0 {
		return nil, usageError("--pr must be a positive integer")
	}
	if (*body == "") == (*bodyFile == "") {
		return nil, usageError("exactly one of --body or --body-file is required")
	}
	if *reviewBody != "" && *reviewBodyFile != "" {
		return nil, usageError("at most one of --review-body or --review-body-file may be provided")
	}

	hasCommentID := *commentID != ""
	hasAnchor := *path != "" || *line != 0 || *side != "" || *startLine != 0 || *startSide != ""
	switch {
	case hasCommentID && hasAnchor:
		return nil, usageError("use either --comment-id or a new-thread anchor, not both")
	case !hasCommentID && !hasAnchor:
		return nil, usageError("either --comment-id or --path/--line/--side is required")
	case hasCommentID:
		if *path != "" || *line != 0 || *side != "" || *startLine != 0 || *startSide != "" {
			return nil, usageError("--comment-id cannot be combined with anchor fields")
		}
	default:
		if *path == "" {
			return nil, usageError("--path is required when targeting by anchor")
		}
		if *line <= 0 {
			return nil, usageError("--line must be a positive integer when targeting by anchor")
		}
		if *side != reviewSideLeft && *side != reviewSideRight {
			return nil, usageError("--side must be LEFT or RIGHT when targeting by anchor")
		}
		if *startLine < 0 {
			return nil, usageError("--start-line must be zero or a positive integer")
		}
		if *startLine == 0 && *startSide != "" {
			return nil, usageError("--start-side requires --start-line")
		}
		if *startLine > 0 && *startSide != reviewSideLeft && *startSide != reviewSideRight {
			return nil, usageError("--start-side must be LEFT or RIGHT when --start-line is set")
		}
	}

	return upsertCommentCommand{
		Repo:            *repo,
		PRNumber:        *prNumber,
		CommentID:       *commentID,
		Path:            *path,
		Line:            *line,
		Side:            *side,
		StartLine:       *startLine,
		StartSide:       *startSide,
		Body:            *body,
		BodyFile:        *bodyFile,
		ConfigDir:       *configDir,
		StateDir:        *stateDir,
		Force:           *force,
		CreateIfMissing: *createIfMissing,
		ReviewBody:      *reviewBody,
		ReviewBodyFile:  *reviewBodyFile,
		JSON:            *jsonOutput,
	}, nil
}

func Usage() string {
	return `gh-pending-review creates and manages pending GitHub pull request reviews.

Usage:
  gh-pending-review serve-proxy [--addr 127.0.0.1:18080]
  gh-pending-review create --pr NUMBER [--repo OWNER/REPO] (--body TEXT | --body-file PATH) [--config-dir DIR] [--state-dir DIR] [--json]
  gh-pending-review delete --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR] [--ensure-absent] [--json]
  gh-pending-review get --pr NUMBER [--repo OWNER/REPO] [--config-dir DIR] [--state-dir DIR] [--pretty]
  gh-pending-review get-state --pr NUMBER [--repo OWNER/REPO] --user LOGIN [--state-dir DIR] [--pretty]
  gh-pending-review replace-comments --pr NUMBER [--repo OWNER/REPO] --comments-file PATH [--config-dir DIR] [--state-dir DIR] [--force] [--create-if-missing] [--review-body TEXT | --review-body-file PATH] [--json]
  gh-pending-review upsert-comment --pr NUMBER [--repo OWNER/REPO] (--comment-id ID | --path PATH --line LINE --side SIDE [--start-line LINE --start-side SIDE]) (--body TEXT | --body-file PATH) [--config-dir DIR] [--state-dir DIR] [--force] [--create-if-missing] [--review-body TEXT | --review-body-file PATH] [--json]
  gh-pending-review delete-state --pr NUMBER [--repo OWNER/REPO] --user LOGIN [--state-dir DIR] [--json]
  gh-pending-review update-body --pr NUMBER [--repo OWNER/REPO] (--body TEXT | --body-file PATH) [--config-dir DIR] [--state-dir DIR] [--force] [--json]
  gh-pending-review version

Notes:
  - serve-proxy runs a loopback-only HTTP proxy for pending-review commands.
  - This tool only manipulates pending reviews owned by the authenticated user.
  - It uses isolated gh auth from --config-dir.
  - It stores the last published review body and comment set locally to detect user edits before update.
  - replace-comments and upsert-comment can create a draft review automatically when passed --create-if-missing.
  - Reads emit compact JSON by default; use --pretty for human-friendly formatting.
  - Mutating commands can emit compact machine-readable success output with --json.
  - It never submits a review.
  - delete --ensure-absent is idempotent: it clears local state, deletes any live pending review, and verifies absence.
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

func UsageError(message string) error {
	return usageError(message)
}

func IsUsageError(err error) bool {
	var target usageErr
	return errors.As(err, &target)
}
