// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatusJSON(t *testing.T) {
	assert := assert.New(t)

	statuses := []Status{
		Committed,
		Aborted,
		Processing,
		Unknown,
		Dropped,
	}
	for _, status := range statuses {
		statusJSON, err := json.Marshal(status)
		assert.NoError(err)

		var parsedStatus Status
		err = json.Unmarshal(statusJSON, &parsedStatus)
		assert.NoError(err)
		assert.Equal(status, parsedStatus)
	}

	{
		status := Status(math.MaxInt32)
		_, err := json.Marshal(status)
		assert.Error(err)
	}

	{
		status := Committed
		err := json.Unmarshal([]byte("null"), &status)
		assert.NoError(err)
		assert.Equal(Committed, status)
	}

	{
		var status Status
		err := json.Unmarshal([]byte(`"not a status"`), &status)
		assert.Error(err)
	}
}

func TestStatusVerify(t *testing.T) {
	assert := assert.New(t)

	statuses := []Status{
		Committed,
		Aborted,
		Processing,
		Unknown,
		Dropped,
	}
	for _, status := range statuses {
		err := status.Verify()
		assert.NoError(err, "%s failed verification", status)
	}

	badStatus := Status(math.MaxInt32)
	err := badStatus.Verify()
	assert.Error(err, "%s passed verification", badStatus)
}

func TestStatusString(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("Committed", Committed.String())
	assert.Equal("Aborted", Aborted.String())
	assert.Equal("Processing", Processing.String())
	assert.Equal("Unknown", Unknown.String())
	assert.Equal("Dropped", Dropped.String())

	badStatus := Status(math.MaxInt32)
	assert.Equal("Invalid status", badStatus.String())
}
