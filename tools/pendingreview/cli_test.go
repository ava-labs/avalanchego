// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package pendingreview

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDeleteCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete", "--pr", "5168"})
	require.NoError(t, err)

	deleteCommand, ok := command.(deleteCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, defaultRepo, deleteCommand.Repo)
	require.Equal(t, 5168, deleteCommand.PRNumber)
	require.NotEmpty(t, deleteCommand.StateDir)
	require.False(t, deleteCommand.EnsureAbsent)
}

func TestParseDeleteCommandEnsureAbsent(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete", "--pr", "5168", "--ensure-absent"})
	require.NoError(t, err)

	deleteCommand, ok := command.(deleteCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, deleteCommand.EnsureAbsent)
}

func TestParseDeleteCommandJSON(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete", "--pr", "5168", "--json"})
	require.NoError(t, err)

	deleteCommand, ok := command.(deleteCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, deleteCommand.JSON)
}

func TestParseGetCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"get", "--pr", "5168"})
	require.NoError(t, err)

	getCommand, ok := command.(getCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, getCommand.PRNumber)
	require.NotEmpty(t, getCommand.StateDir)
}

func TestParseGetCommandPretty(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"get", "--pr", "5168", "--pretty"})
	require.NoError(t, err)

	getCommand, ok := command.(getCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, getCommand.Pretty)
}

func TestParseUpdateBodyCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body", "test"})
	require.NoError(t, err)

	updateBodyCommand, ok := command.(updateBodyCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, updateBodyCommand.PRNumber)
	require.Equal(t, "test", updateBodyCommand.Body)
	require.NotEmpty(t, updateBodyCommand.StateDir)
	require.False(t, updateBodyCommand.Force)
}

func TestParseUpdateBodyCommandForce(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body", "test", "--force"})
	require.NoError(t, err)

	updateBodyCommand, ok := command.(updateBodyCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, updateBodyCommand.Force)
}

func TestParseUpdateBodyCommandJSON(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body", "test", "--json"})
	require.NoError(t, err)

	updateBodyCommand, ok := command.(updateBodyCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, updateBodyCommand.JSON)
}

func TestParseCreateCommandBodyFile(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"create", "--pr", "5168", "--body-file", "/tmp/body.txt"})
	require.NoError(t, err)

	createCommand, ok := command.(createCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, createCommand.PRNumber)
	require.Equal(t, "/tmp/body.txt", createCommand.BodyFile)
	require.Empty(t, createCommand.Body)
}

func TestParseUpdateBodyCommandBodyFile(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"update-body", "--pr", "5168", "--body-file", "/tmp/body.txt"})
	require.NoError(t, err)

	updateBodyCommand, ok := command.(updateBodyCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, updateBodyCommand.PRNumber)
	require.Equal(t, "/tmp/body.txt", updateBodyCommand.BodyFile)
	require.Empty(t, updateBodyCommand.Body)
}

func TestParseCreateCommandRejectsBodyAndBodyFile(t *testing.T) {
	t.Parallel()

	_, err := parseCommand([]string{"create", "--pr", "5168", "--body", "inline", "--body-file", "/tmp/body.txt"})
	require.EqualError(t, err, "exactly one of --body or --body-file is required\n\n"+Usage())
}

func TestParseReplaceCommentsCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"replace-comments", "--pr", "5168", "--comments-file", "/tmp/comments.json", "--force"})
	require.NoError(t, err)

	replaceCommentsCommand, ok := command.(replaceCommentsCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, replaceCommentsCommand.PRNumber)
	require.Equal(t, "/tmp/comments.json", replaceCommentsCommand.CommentsFile)
	require.True(t, replaceCommentsCommand.Force)
}

func TestParseReplaceCommentsCommandJSON(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"replace-comments", "--pr", "5168", "--comments-file", "/tmp/comments.json", "--json"})
	require.NoError(t, err)

	replaceCommentsCommand, ok := command.(replaceCommentsCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, replaceCommentsCommand.JSON)
}

func TestParseReplaceCommentsCommandCreateIfMissing(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"replace-comments", "--pr", "5168", "--comments-file", "/tmp/comments.json", "--create-if-missing", "--review-body", "inline"})
	require.NoError(t, err)

	replaceCommentsCommand, ok := command.(replaceCommentsCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, replaceCommentsCommand.CreateIfMissing)
	require.Equal(t, "inline", replaceCommentsCommand.ReviewBody)
}

func TestParseUpsertCommentCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"upsert-comment", "--pr", "5168", "--path", "a.go", "--line", "7", "--side", "RIGHT", "--body", "hello"})
	require.NoError(t, err)

	upsertCommentCommand, ok := command.(upsertCommentCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, upsertCommentCommand.PRNumber)
	require.Equal(t, "a.go", upsertCommentCommand.Path)
	require.Equal(t, 7, upsertCommentCommand.Line)
	require.Equal(t, "RIGHT", upsertCommentCommand.Side)
	require.Equal(t, "hello", upsertCommentCommand.Body)
}

func TestParseUpsertCommentCommandByCommentID(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"upsert-comment", "--pr", "5168", "--comment-id", "comment-1", "--body-file", "/tmp/body.txt", "--create-if-missing"})
	require.NoError(t, err)

	upsertCommentCommand, ok := command.(upsertCommentCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, "comment-1", upsertCommentCommand.CommentID)
	require.Equal(t, "/tmp/body.txt", upsertCommentCommand.BodyFile)
	require.True(t, upsertCommentCommand.CreateIfMissing)
}

func TestParseUpsertCommentCommandJSON(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"upsert-comment", "--pr", "5168", "--path", "a.go", "--line", "7", "--side", "RIGHT", "--body", "hello", "--json"})
	require.NoError(t, err)

	upsertCommentCommand, ok := command.(upsertCommentCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, upsertCommentCommand.JSON)
}

func TestParseGetStateCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"get-state", "--pr", "5168", "--user", "maru"})
	require.NoError(t, err)

	getStateCommand, ok := command.(getStateCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, getStateCommand.PRNumber)
	require.Equal(t, "maru", getStateCommand.UserLogin)
	require.NotEmpty(t, getStateCommand.StateDir)
}

func TestParseGetStateCommandPretty(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"get-state", "--pr", "5168", "--user", "maru", "--pretty"})
	require.NoError(t, err)

	getStateCommand, ok := command.(getStateCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, getStateCommand.Pretty)
}

func TestParseDeleteStateCommand(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete-state", "--pr", "5168", "--user", "maru"})
	require.NoError(t, err)

	deleteStateCommand, ok := command.(deleteStateCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.Equal(t, 5168, deleteStateCommand.PRNumber)
	require.Equal(t, "maru", deleteStateCommand.UserLogin)
	require.NotEmpty(t, deleteStateCommand.StateDir)
}

func TestParseDeleteStateCommandJSON(t *testing.T) {
	t.Parallel()

	command, err := parseCommand([]string{"delete-state", "--pr", "5168", "--user", "maru", "--json"})
	require.NoError(t, err)

	deleteStateCommand, ok := command.(deleteStateCommand)
	require.True(t, ok, "unexpected command type %T", command)
	require.True(t, deleteStateCommand.JSON)
}
