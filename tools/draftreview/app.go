package draftreview

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	tokenProvider TokenProvider
	httpClient    HTTPDoer
	baseURL       string
	log           logging.Logger
}

func NewApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *App {
	log := newDebugLogger(stderr)
	return &App{
		Stdin:         stdin,
		Stdout:        stdout,
		Stderr:        stderr,
		tokenProvider: NewGHTokenProvider(log),
		httpClient:    DefaultHTTPClient(),
		baseURL:       defaultGitHubAPIBaseURL,
		log:           log,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	command, err := parseCommand(args)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Debug("running command",
		zap.String("command", commandName(command)),
	)

	switch command := command.(type) {
	case createCommand:
		return a.runCreate(ctx, command)
	case deleteCommand:
		return a.runDelete(ctx, command)
	case getCommand:
		return a.runGet(ctx, command)
	case updateBodyCommand:
		return a.runUpdateBody(ctx, command)
	case versionCommand:
		return a.runVersion(command)
	default:
		return stacktrace.Errorf("unsupported command type %T", command)
	}
}

func (a *App) runCreate(ctx context.Context, command createCommand) error {
	a.log.Debug("entered create command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
		zap.Int("bodyLength", len(command.Body)),
	)
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	client := NewGitHubClient(a.log, a.httpClient, a.baseURL, token)
	a.log.Info("creating pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.Int("bodyLength", len(command.Body)),
	)
	review, err := client.CreatePendingReview(ctx, command.Repo, command.PRNumber, command.Body)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if err := NewStateStore(a.log, command.StateDir).Save(ReviewState{
		Repo:              command.Repo,
		PRNumber:          command.PRNumber,
		UserLogin:         review.User.Login,
		ReviewID:          review.ID,
		LastPublishedBody: review.Body,
		HTMLURL:           review.HTMLURL,
	}); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("created pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.Int64("reviewID", review.ID),
		zap.String("userLogin", review.User.Login),
		zap.String("state", review.State),
		zap.String("htmlURL", review.HTMLURL),
	)

	if _, err := fmt.Fprintf(a.Stdout, "Created pending review %d for %s#%d (%s)\n", review.ID, command.Repo, command.PRNumber, review.State); err != nil {
		return stacktrace.Wrap(err)
	}
	if review.HTMLURL != "" {
		if _, err := fmt.Fprintln(a.Stdout, review.HTMLURL); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
}

func (a *App) runDelete(ctx context.Context, command deleteCommand) error {
	a.log.Debug("entered delete command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
	)
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	client := NewGitHubClient(a.log, a.httpClient, a.baseURL, token)
	viewer, err := client.Viewer(ctx)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	reviews, err := client.ListReviews(ctx, command.Repo, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	if !found {
		return stacktrace.Errorf("no pending review found for %s on %s#%d", viewer.Login, command.Repo, command.PRNumber)
	}

	if err := client.DeletePendingReview(ctx, command.Repo, command.PRNumber, review.ID); err != nil {
		return stacktrace.Wrap(err)
	}

	if err := NewStateStore(a.log, command.StateDir).Delete(command.Repo, viewer.Login, command.PRNumber); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("deleted pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.Int64("reviewID", review.ID),
		zap.String("userLogin", viewer.Login),
	)

	_, err = fmt.Fprintf(a.Stdout, "Deleted pending review %d for %s#%d\n", review.ID, command.Repo, command.PRNumber)
	return stacktrace.Wrap(err)
}

func (a *App) runGet(ctx context.Context, command getCommand) error {
	a.log.Debug("entered get command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
	)
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	client := NewGitHubClient(a.log, a.httpClient, a.baseURL, token)
	viewer, err := client.Viewer(ctx)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	reviews, err := client.ListReviews(ctx, command.Repo, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	if !found {
		return stacktrace.Errorf("no pending review found for %s on %s#%d", viewer.Login, command.Repo, command.PRNumber)
	}

	a.log.Info("fetched pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.Int64("reviewID", review.ID),
		zap.String("userLogin", viewer.Login),
		zap.String("state", review.State),
	)

	encoded, err := json.MarshalIndent(review, "", "  ")
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if _, err := fmt.Fprintln(a.Stdout, string(encoded)); err != nil {
		return stacktrace.Wrap(err)
	}
	return nil
}

func (a *App) runUpdateBody(ctx context.Context, command updateBodyCommand) error {
	a.log.Debug("entered update-body command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
		zap.Bool("force", command.Force),
		zap.Int("bodyLength", len(command.Body)),
	)
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	client := NewGitHubClient(a.log, a.httpClient, a.baseURL, token)
	viewer, err := client.Viewer(ctx)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	reviews, err := client.ListReviews(ctx, command.Repo, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	if !found {
		return stacktrace.Errorf("no pending review found for %s on %s#%d", viewer.Login, command.Repo, command.PRNumber)
	}

	store := NewStateStore(a.log, command.StateDir)
	if !command.Force {
		state, err := store.Load(command.Repo, viewer.Login, command.PRNumber)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		if state.ReviewID != 0 && state.ReviewID != review.ID {
			return stacktrace.Errorf("stored review state points to review %d but GitHub has pending review %d; run get, reconcile, then retry with --force if intended", state.ReviewID, review.ID)
		}
		if state.LastPublishedBody != review.Body {
			a.log.Info("refusing pending review body update because stored state diverged",
				zap.String("repo", command.Repo),
				zap.Int("prNumber", command.PRNumber),
				zap.String("userLogin", viewer.Login),
				zap.Int64("reviewID", review.ID),
				zap.Bool("force", command.Force),
			)
			return stacktrace.Wrap(ErrReviewConflict)
		}
	}

	a.log.Info("updating pending review body",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.Int64("reviewID", review.ID),
		zap.Bool("force", command.Force),
		zap.Int("bodyLength", len(command.Body)),
	)
	updated, err := client.UpdatePendingReviewBody(ctx, command.Repo, command.PRNumber, review.ID, command.Body)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if err := store.Save(ReviewState{
		Repo:              command.Repo,
		PRNumber:          command.PRNumber,
		UserLogin:         viewer.Login,
		ReviewID:          updated.ID,
		LastPublishedBody: updated.Body,
		HTMLURL:           updated.HTMLURL,
	}); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("updated pending review body",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.Int64("reviewID", updated.ID),
		zap.String("state", updated.State),
		zap.String("htmlURL", updated.HTMLURL),
	)

	_, err = fmt.Fprintf(a.Stdout, "Updated pending review %d for %s#%d (%s)\n", updated.ID, command.Repo, command.PRNumber, updated.State)
	return stacktrace.Wrap(err)
}

func (a *App) runVersion(_ versionCommand) error {
	_, err := fmt.Fprintln(a.Stdout, VersionString())
	return stacktrace.Wrap(err)
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	return stacktrace.Wrap(NewApp(stdin, stdout, stderr).Run(ctx, args))
}

var ErrReviewConflict = stacktrace.New("pending review body no longer matches stored state; run get, reconcile the current review body, then retry with --force if you intend to overwrite it")

func commandName(cmd command) string {
	switch cmd.(type) {
	case createCommand:
		return "create"
	case deleteCommand:
		return "delete"
	case getCommand:
		return "get"
	case updateBodyCommand:
		return "update-body"
	case versionCommand:
		return "version"
	default:
		return fmt.Sprintf("%T", cmd)
	}
}
