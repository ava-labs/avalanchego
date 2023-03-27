// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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
		err = json.Unmarshal(statusJSON, &parsedStatus)
		require.NoError(err)
		require.Equal(status, parsedStatus)
	}

	{
		status := Status(math.MaxInt32)
		_, err := json.Marshal(status)
		require.Error(err)
	}

	{
		status := Committed
		err := json.Unmarshal([]byte("null"), &status)
		require.NoError(err)
		require.Equal(Committed, status)
	}

	{
		var status Status
		err := json.Unmarshal([]byte(`"not a status"`), &status)
		require.Error(err)
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
	require.Error(err, "%s passed verification", badStatus)
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
