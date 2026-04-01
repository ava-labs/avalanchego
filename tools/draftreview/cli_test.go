// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package draftreview

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
