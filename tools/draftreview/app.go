package draftreview

import (
	"context"
	"fmt"
	"io"
)

type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	tokenProvider TokenProvider
	httpClient    HTTPDoer
	baseURL       string
}

func NewApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *App {
	return &App{
		Stdin:         stdin,
		Stdout:        stdout,
		Stderr:        stderr,
		tokenProvider: NewGHTokenProvider(),
		httpClient:    DefaultHTTPClient(),
		baseURL:       defaultGitHubAPIBaseURL,
	}
}

func (a *App) Run(ctx context.Context, args []string) error {
	command, err := parseCommand(args)
	if err != nil {
		return err
	}

	switch command := command.(type) {
	case createCommand:
		return a.runCreate(ctx, command)
	case deleteCommand:
		return a.runDelete(ctx, command)
	case versionCommand:
		return a.runVersion(command)
	default:
		return fmt.Errorf("unsupported command type %T", command)
	}
}

func (a *App) runCreate(ctx context.Context, command createCommand) error {
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return err
	}

	client := NewGitHubClient(a.httpClient, a.baseURL, token)
	review, err := client.CreatePendingReview(ctx, command.Repo, command.PRNumber, command.Body)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(a.Stdout, "Created pending review %d for %s#%d (%s)\n", review.ID, command.Repo, command.PRNumber, review.State); err != nil {
		return err
	}
	if review.HTMLURL != "" {
		if _, err := fmt.Fprintln(a.Stdout, review.HTMLURL); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) runDelete(ctx context.Context, command deleteCommand) error {
	token, err := a.tokenProvider.Token(ctx, command.ConfigDir)
	if err != nil {
		return err
	}

	client := NewGitHubClient(a.httpClient, a.baseURL, token)
	viewer, err := client.Viewer(ctx)
	if err != nil {
		return err
	}

	reviews, err := client.ListReviews(ctx, command.Repo, command.PRNumber)
	if err != nil {
		return err
	}

	review, found := FindPendingReviewForAuthor(reviews, viewer.Login)
	if !found {
		return fmt.Errorf("no pending review found for %s on %s#%d", viewer.Login, command.Repo, command.PRNumber)
	}

	if err := client.DeletePendingReview(ctx, command.Repo, command.PRNumber, review.ID); err != nil {
		return err
	}

	_, err = fmt.Fprintf(a.Stdout, "Deleted pending review %d for %s#%d\n", review.ID, command.Repo, command.PRNumber)
	return err
}

func (a *App) runVersion(_ versionCommand) error {
	_, err := fmt.Fprintln(a.Stdout, VersionString())
	return err
}

func Run(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) error {
	return NewApp(stdin, stdout, stderr).Run(ctx, args)
}
