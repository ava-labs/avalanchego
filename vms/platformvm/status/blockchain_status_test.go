// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package status

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBlockchainStatusJSON(t *testing.T) {
	assert := assert.New(t)

	statuses := []BlockchainStatus{
		Validating,
		Created,
		Preferred,
		Syncing,
	}
	for _, status := range statuses {
		statusJSON, err := json.Marshal(status)
		assert.NoError(err)

		var parsedStatus BlockchainStatus
		err = json.Unmarshal(statusJSON, &parsedStatus)
		assert.NoError(err)
		assert.Equal(status, parsedStatus)
	}

	{
		status := BlockchainStatus(math.MaxInt32)
		_, err := json.Marshal(status)
		assert.Error(err)
	}

	{
		status := Validating
		err := json.Unmarshal([]byte("null"), &status)
		assert.NoError(err)
		assert.Equal(Validating, status)
	}

	{
		var status BlockchainStatus
		err := json.Unmarshal([]byte(`"not a status"`), &status)
		assert.Error(err)
	}
}

func TestBlockchainStatusVerify(t *testing.T) {
	assert := assert.New(t)

	statuses := []BlockchainStatus{
		Validating,
		Created,
		Preferred,
		Syncing,
	}
	for _, status := range statuses {
		err := status.Verify()
		assert.NoError(err, "%s failed verification", status)
	}

	badStatus := BlockchainStatus(math.MaxInt32)
	err := badStatus.Verify()
	assert.Error(err, "%s passed verification", badStatus)
}

func TestBlockchainStatusString(t *testing.T) {
	assert := assert.New(t)

	assert.Equal("Validating", Validating.String())
	assert.Equal("Created", Created.String())
	assert.Equal("Preferred", Preferred.String())
	assert.Equal("Syncing", Syncing.String())
	assert.Equal("Dropped", Dropped.String())

	badStatus := BlockchainStatus(math.MaxInt32)
	assert.Equal("Invalid blockchain status", badStatus.String())
}
