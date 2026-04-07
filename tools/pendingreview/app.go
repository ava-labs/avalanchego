// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

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
	baseURL := defaultGitHubAPIBaseURL
	if override := os.Getenv("GH_PENDING_REVIEW_BASE_URL"); override != "" {
		baseURL = override
	}
	return &App{
		Stdin:         stdin,
		Stdout:        stdout,
		Stderr:        stderr,
		tokenProvider: NewGHTokenProvider(log),
		httpClient:    DefaultHTTPClient(),
		baseURL:       baseURL,
		log:           log,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	command, err := parseCommand(args)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Debug("running command", zap.String("command", commandName(command)))

	switch command := command.(type) {
	case createCommand:
		return a.runCreate(ctx, command)
	case deleteCommand:
		return a.runDelete(ctx, command)
	case getCommand:
		return a.runGet(ctx, command)
	case getStateCommand:
		return a.runGetState(command)
	case replaceCommentsCommand:
		return a.runReplaceComments(ctx, command)
	case deleteStateCommand:
		return a.runDeleteState(command)
	case updateBodyCommand:
		return a.runUpdateBody(ctx, command)
	case versionCommand:
		return a.runVersion(command)
	default:
		return stacktrace.Errorf("unsupported command type %T", command)
	}
}

func (a *App) runCreate(ctx context.Context, command createCommand) error {
	body, err := resolveBodyInput(command.Body, command.BodyFile)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	a.log.Debug("entered create command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
		zap.Int("bodyLength", len(body)),
	)

	client, err := a.newClient(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("creating pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.Int("bodyLength", len(body)),
	)
	review, err := client.CreatePendingReview(ctx, command.Repo, command.PRNumber, body)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	if err := NewStateStore(a.log, command.StateDir).Save(ReviewState{
		Repo:                 command.Repo,
		PRNumber:             command.PRNumber,
		UserLogin:            review.User.Login,
		ReviewID:             review.ID,
		LastPublishedBody:    review.Body,
		LastPublishedEntries: nil,
		HTMLURL:              review.HTMLURL,
	}); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("created pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("reviewID", review.ID),
		zap.String("userLogin", review.User.Login),
		zap.String("state", review.State),
		zap.String("htmlURL", review.HTMLURL),
	)

	if _, err := fmt.Fprintf(a.Stdout, "Created pending review %s for %s#%d (%s)\n", displayReviewID(review), command.Repo, command.PRNumber, review.State); err != nil {
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
		zap.Bool("ensureAbsent", command.EnsureAbsent),
	)

	client, viewer, err := a.newClientAndViewer(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	store := NewStateStore(a.log, command.StateDir)
	review, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login)
	if err != nil {
		if !command.EnsureAbsent || !errors.Is(err, ErrNoPendingReview) {
			return stacktrace.Wrap(err)
		}
		if err := store.Delete(command.Repo, viewer.Login, command.PRNumber); err != nil {
			return stacktrace.Wrap(err)
		}

		a.log.Info("verified no pending review remains",
			zap.String("repo", command.Repo),
			zap.Int("prNumber", command.PRNumber),
			zap.String("userLogin", viewer.Login),
		)
		_, err = fmt.Fprintf(a.Stdout, "No pending review found for %s#%d as %s; verified no pending review remains and cleared stored state if present.\n", command.Repo, command.PRNumber, viewer.Login)
		return stacktrace.Wrap(err)
	}
	if err := client.DeletePendingReview(ctx, review.ID); err != nil {
		return stacktrace.Wrap(err)
	}
	if err := store.Delete(command.Repo, viewer.Login, command.PRNumber); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("deleted pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("reviewID", review.ID),
		zap.String("userLogin", viewer.Login),
	)

	if !command.EnsureAbsent {
		_, err = fmt.Fprintf(a.Stdout, "Deleted pending review %s for %s#%d\n", displayReviewID(review), command.Repo, command.PRNumber)
		return stacktrace.Wrap(err)
	}

	if _, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login); err != nil {
		if errors.Is(err, ErrNoPendingReview) {
			a.log.Info("verified no pending review remains",
				zap.String("repo", command.Repo),
				zap.Int("prNumber", command.PRNumber),
				zap.String("userLogin", viewer.Login),
			)
			_, err = fmt.Fprintf(a.Stdout, "Deleted pending review %s for %s#%d\nVerified no pending review remains for %s#%d as %s.\n", displayReviewID(review), command.Repo, command.PRNumber, command.Repo, command.PRNumber, viewer.Login)
			return stacktrace.Wrap(err)
		}
		return stacktrace.Wrap(err)
	}
	return stacktrace.Errorf("pending review for %s#%d as %s still exists after delete", command.Repo, command.PRNumber, viewer.Login)
}

func (a *App) runGet(ctx context.Context, command getCommand) error {
	a.log.Debug("entered get command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
	)

	client, viewer, err := a.newClientAndViewer(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("fetched pending review",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("reviewID", review.ID),
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

func (a *App) runGetState(command getStateCommand) error {
	state, err := NewStateStore(a.log, command.StateDir).Load(command.Repo, command.UserLogin, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	encoded, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if _, err := fmt.Fprintln(a.Stdout, string(encoded)); err != nil {
		return stacktrace.Wrap(err)
	}
	return nil
}

func (a *App) runUpdateBody(ctx context.Context, command updateBodyCommand) error {
	body, err := resolveBodyInput(command.Body, command.BodyFile)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	a.log.Debug("entered update-body command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
		zap.Bool("force", command.Force),
		zap.Int("bodyLength", len(body)),
	)

	client, viewer, err := a.newClientAndViewer(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	a.log.Debug("fetched live pending review for body update",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", review.ID),
		zap.Int("liveBodyLength", len(review.Body)),
		zap.Int("liveCommentCount", len(review.Comments)),
	)

	store := NewStateStore(a.log, command.StateDir)
	if !command.Force {
		state, err := store.Load(command.Repo, viewer.Login, command.PRNumber)
		if err != nil {
			return stacktrace.Wrap(err)
		}
		a.log.Debug("loaded stored pending review state for body update",
			zap.String("repo", command.Repo),
			zap.Int("prNumber", command.PRNumber),
			zap.String("userLogin", viewer.Login),
			zap.String("storedReviewID", state.ReviewID),
			zap.Int("storedBodyLength", len(state.LastPublishedBody)),
			zap.Int("storedEntryCount", len(state.LastPublishedEntries)),
			zap.Bool("reviewIDMatches", state.ReviewID == "" || state.ReviewID == review.ID),
			zap.Bool("bodyMatches", state.LastPublishedBody == review.Body),
			zap.Int("desiredBodyLength", len(body)),
		)
		if state.ReviewID != "" && state.ReviewID != review.ID {
			return stacktrace.Errorf("stored review state points to review %s but GitHub has pending review %s; run get, reconcile, then retry with --force if intended", state.ReviewID, displayReviewID(review))
		}
		if state.LastPublishedBody != review.Body {
			a.log.Info("refusing pending review body update because stored state diverged",
				zap.String("repo", command.Repo),
				zap.Int("prNumber", command.PRNumber),
				zap.String("userLogin", viewer.Login),
				zap.String("reviewID", review.ID),
				zap.Bool("force", command.Force),
			)
			return stacktrace.Wrap(ErrReviewConflict)
		}
	}

	storedState, _, err := store.LoadIfExists(command.Repo, viewer.Login, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	a.log.Debug("applying pending review body update",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", review.ID),
		zap.Int("liveBodyLength", len(review.Body)),
		zap.Int("desiredBodyLength", len(body)),
		zap.Int("preservedEntryCount", len(storedState.LastPublishedEntries)),
		zap.Bool("force", command.Force),
	)

	updated, err := client.UpdatePendingReviewBody(ctx, review.ID, body)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if err := store.Save(ReviewState{
		Repo:                 command.Repo,
		PRNumber:             command.PRNumber,
		UserLogin:            viewer.Login,
		ReviewID:             updated.ID,
		LastPublishedBody:    updated.Body,
		LastPublishedEntries: storedState.LastPublishedEntries,
		HTMLURL:              updated.HTMLURL,
	}); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("updated pending review body",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", updated.ID),
		zap.String("state", updated.State),
		zap.String("htmlURL", updated.HTMLURL),
	)

	_, err = fmt.Fprintf(a.Stdout, "Updated pending review %s for %s#%d (%s)\n", displayReviewID(updated), command.Repo, command.PRNumber, updated.State)
	return stacktrace.Wrap(err)
}

func (a *App) runDeleteState(command deleteStateCommand) error {
	if err := NewStateStore(a.log, command.StateDir).Delete(command.Repo, command.UserLogin, command.PRNumber); err != nil {
		return stacktrace.Wrap(err)
	}
	_, err := fmt.Fprintf(a.Stdout, "Deleted stored review state for %s#%d as %s\n", command.Repo, command.PRNumber, command.UserLogin)
	return stacktrace.Wrap(err)
}

func (a *App) runReplaceComments(ctx context.Context, command replaceCommentsCommand) error {
	a.log.Debug("entered replace-comments command",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("commentsFile", command.CommentsFile),
		zap.String("configDir", command.ConfigDir),
		zap.String("stateDir", command.StateDir),
		zap.Bool("force", command.Force),
	)

	entries, err := loadCommentsFile(command.CommentsFile)
	if err != nil {
		return stacktrace.Wrap(formatCommentsPathError(command.CommentsFile, err))
	}

	client, viewer, err := a.newClientAndViewer(ctx, command.ConfigDir)
	if err != nil {
		return stacktrace.Wrap(err)
	}

	review, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	liveManagedEntries := normalizeLiveReviewComments(review.Comments, viewer.Login)
	a.log.Debug("fetched live pending review for comment replace",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", review.ID),
		zap.Int("liveBodyLength", len(review.Body)),
		zap.Int("liveManagedEntryCount", len(liveManagedEntries)),
		zap.Int("desiredEntryCount", len(entries)),
	)

	store := NewStateStore(a.log, command.StateDir)
	storedState, foundStoredState, err := store.LoadIfExists(command.Repo, viewer.Login, command.PRNumber)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	a.log.Debug("loaded stored pending review state for comment replace",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.Bool("foundStoredState", foundStoredState),
		zap.String("storedReviewID", storedState.ReviewID),
		zap.Int("storedBodyLength", len(storedState.LastPublishedBody)),
		zap.Int("storedEntryCount", len(storedState.LastPublishedEntries)),
		zap.Bool("force", command.Force),
	)
	if !foundStoredState && !command.Force {
		return stacktrace.Errorf("no stored review state for %s#%d as %s; run create first or use --force", command.Repo, command.PRNumber, viewer.Login)
	}
	if foundStoredState && storedState.ReviewID != "" && storedState.ReviewID != review.ID && !command.Force {
		return stacktrace.Errorf("stored review state points to review %s but GitHub has pending review %s; run get, reconcile, then retry with --force if intended", storedState.ReviewID, displayReviewID(review))
	}
	if !command.Force {
		storedEntries := normalizeDraftReviewEntries(storedState.LastPublishedEntries)
		a.log.Debug("comparing stored and live managed entries",
			zap.String("repo", command.Repo),
			zap.Int("prNumber", command.PRNumber),
			zap.String("userLogin", viewer.Login),
			zap.String("reviewID", review.ID),
			zap.Int("storedEntryCount", len(storedEntries)),
			zap.Int("liveManagedEntryCount", len(liveManagedEntries)),
			zap.Bool("entriesMatch", draftReviewEntriesEqual(storedEntries, liveManagedEntries)),
			zap.Int("desiredEntryCount", len(entries)),
		)
		if !draftReviewEntriesEqual(storedEntries, liveManagedEntries) {
			a.log.Info("refusing pending review comment replace because stored state diverged",
				zap.String("repo", command.Repo),
				zap.Int("prNumber", command.PRNumber),
				zap.String("userLogin", viewer.Login),
				zap.String("reviewID", review.ID),
				zap.String("diff", diffCommentsMessage(liveManagedEntries, storedEntries)),
			)
			return stacktrace.Wrap(ErrReviewCommentsConflict)
		}
	}

	a.log.Debug("applying pending review comment replace",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", review.ID),
		zap.Int("existingCommentCount", len(review.Comments)),
		zap.Int("desiredEntryCount", len(entries)),
		zap.Bool("force", command.Force),
	)
	if err := client.ReplacePendingReviewEntries(ctx, review.ID, review.Comments, entries); err != nil {
		return stacktrace.Wrap(err)
	}
	updatedReview, err := client.GetPendingReview(ctx, command.Repo, command.PRNumber, viewer.Login)
	if err != nil {
		return stacktrace.Wrap(err)
	}
	if err := store.Save(ReviewState{
		Repo:                 command.Repo,
		PRNumber:             command.PRNumber,
		UserLogin:            viewer.Login,
		ReviewID:             updatedReview.ID,
		LastPublishedBody:    updatedReview.Body,
		LastPublishedEntries: normalizeLiveReviewComments(updatedReview.Comments, viewer.Login),
		HTMLURL:              updatedReview.HTMLURL,
	}); err != nil {
		return stacktrace.Wrap(err)
	}

	a.log.Info("replaced pending review comments",
		zap.String("repo", command.Repo),
		zap.Int("prNumber", command.PRNumber),
		zap.String("userLogin", viewer.Login),
		zap.String("reviewID", updatedReview.ID),
		zap.Int("commentCount", len(entries)),
		zap.String("htmlURL", updatedReview.HTMLURL),
	)

	if _, err := fmt.Fprintf(a.Stdout, "Replaced comments for pending review %s on %s#%d (%s)\n", displayReviewID(updatedReview), command.Repo, command.PRNumber, updatedReview.State); err != nil {
		return stacktrace.Wrap(err)
	}
	if updatedReview.HTMLURL != "" {
		if _, err := fmt.Fprintln(a.Stdout, updatedReview.HTMLURL); err != nil {
			return stacktrace.Wrap(err)
		}
	}
	return nil
}

func (a *App) runVersion(_ versionCommand) error {
	_, err := fmt.Fprintln(a.Stdout, VersionString())
	return stacktrace.Wrap(err)
}

func (a *App) newClient(ctx context.Context, configDir string) (*GitHubClient, error) {
	token, err := a.tokenProvider.Token(ctx, configDir)
	if err != nil {
		return nil, stacktrace.Wrap(err)
	}
	return NewGitHubClient(a.log, a.httpClient, a.baseURL, token), nil
}

func (a *App) newClientAndViewer(ctx context.Context, configDir string) (*GitHubClient, User, error) {
	client, err := a.newClient(ctx, configDir)
	if err != nil {
		return nil, User{}, err
	}
	viewer, err := client.Viewer(ctx)
	if err != nil {
		return nil, User{}, stacktrace.Wrap(err)
	}
	return client, viewer, nil
}

func displayReviewID(review Review) string {
	if review.DatabaseID != 0 {
		return strconv.FormatInt(review.DatabaseID, 10)
	}
	if review.ID != "" {
		return review.ID
	}
	return "unknown"
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	return stacktrace.Wrap(NewApp(stdin, stdout, stderr).Run(ctx, args))
}

var (
	ErrNoPendingReview = stacktrace.New(
		"no pending review found",
	)
	ErrReviewConflict = stacktrace.New(
		"pending review body no longer matches stored state; run get, reconcile the current review body, then retry with --force if you intend to overwrite it",
	)
	ErrReviewCommentsConflict = stacktrace.New(
		"pending review comments no longer match stored state; run get, reconcile the current review body and comments, then retry with --force if you intend to overwrite them",
	)
)

func commandName(cmd command) string {
	switch cmd.(type) {
	case createCommand:
		return "create"
	case deleteCommand:
		return "delete"
	case getCommand:
		return "get"
	case getStateCommand:
		return "get-state"
	case replaceCommentsCommand:
		return "replace-comments"
	case deleteStateCommand:
		return "delete-state"
	case updateBodyCommand:
		return "update-body"
	case versionCommand:
		return "version"
	default:
		return fmt.Sprintf("%T", cmd)
	}
}

func resolveBodyInput(body string, bodyFile string) (string, error) {
	switch {
	case (body == "") == (bodyFile == ""):
		return "", stacktrace.New("exactly one of --body or --body-file is required")
	case bodyFile == "":
		return body, nil
	}

	content, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", stacktrace.Wrap(err)
	}
	if len(content) == 0 {
		return "", stacktrace.New("review body must not be empty")
	}
	return string(content), nil
}
