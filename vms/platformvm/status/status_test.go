// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusJSON(t *testing.T) {
	require := require.New(t)

	statuses := []Status{
		Committed,
		Aborted,
		Processing,
		Unknown,
		Dropped,
	}
	for _, status := range statuses {
		statusJSON, err := json.Marshal(status)
		require.NoError(err)

		var parsedStatus Status
		require.NoError(json.Unmarshal(statusJSON, &parsedStatus))
		require.Equal(status, parsedStatus)
	}

	{
		status := Status(math.MaxInt32)
		_, err := json.Marshal(status)
		require.ErrorIs(err, errUnknownStatus)
	}

	{
		status := Committed
		require.NoError(json.Unmarshal([]byte("null"), &status))
		require.Equal(Committed, status)
	}

	{
		var status Status
		err := json.Unmarshal([]byte(`"not a status"`), &status)
		require.ErrorIs(err, errUnknownStatus)
	}
}

func TestStatusVerify(t *testing.T) {
	require := require.New(t)

	statuses := []Status{
		Committed,
		Aborted,
		Processing,
		Unknown,
		Dropped,
	}
	for _, status := range statuses {
		err := status.Verify()
		require.NoError(err, "%s failed verification", status)
	}

	badStatus := Status(math.MaxInt32)
	err := badStatus.Verify()
	require.ErrorIs(err, errUnknownStatus)
}

func TestStatusString(t *testing.T) {
	require := require.New(t)

	require.Equal("Committed", Committed.String())
	require.Equal("Aborted", Aborted.String())
	require.Equal("Processing", Processing.String())
	require.Equal("Unknown", Unknown.String())
	require.Equal("Dropped", Dropped.String())

	badStatus := Status(math.MaxInt32)
	require.Equal("Invalid status", badStatus.String())
}
