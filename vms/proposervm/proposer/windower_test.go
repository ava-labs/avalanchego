// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposer

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
)

func TestWindowerNoValidators(t *testing.T) {
	assert := assert.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	nodeID := ids.GenerateTestShortID()
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
			return nil, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	delay, err := w.Delay(1, 0, nodeID)
	assert.NoError(err)
	assert.EqualValues(0, delay)
}

func TestWindowerRepeatedValidator(t *testing.T) {
	assert := assert.New(t)

	subnetID := ids.GenerateTestID()
	chainID := ids.GenerateTestID()
	validatorID := ids.GenerateTestShortID()
	nonValidatorID := ids.GenerateTestShortID()
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
			return map[ids.ShortID]uint64{
				validatorID: 10,
			}, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	validatorDelay, err := w.Delay(1, 0, validatorID)
	assert.NoError(err)
	assert.EqualValues(0, validatorDelay)

	nonValidatorDelay, err := w.Delay(1, 0, nonValidatorID)
	assert.NoError(err)
	assert.EqualValues(MaxDelay, nonValidatorDelay)
}

func TestWindowerChangeByHeight(t *testing.T) {
	assert := assert.New(t)

	subnetID := ids.ID{0, 1}
	chainID := ids.ID{0, 2}
	validatorIDs := make([]ids.ShortID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.ShortID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
			validators := make(map[ids.ShortID]uint64, MaxWindows)
			for _, id := range validatorIDs {
				validators[id] = 1
			}
			return validators, nil
		},
	}

	w := New(vdrState, subnetID, chainID)

	expectedDelays1 := []time.Duration{
		2 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		1 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		fmt.Println(vdrID)
		validatorDelay, err := w.Delay(1, 0, vdrID)
		assert.NoError(err)
		assert.EqualValues(expectedDelay, validatorDelay)
	}

	expectedDelays2 := []time.Duration{
		5 * WindowDuration,
		1 * WindowDuration,
		3 * WindowDuration,
		4 * WindowDuration,
		0 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays2 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w.Delay(2, 0, vdrID)
		assert.NoError(err)
		assert.EqualValues(expectedDelay, validatorDelay)
	}
}

func TestWindowerChangeByChain(t *testing.T) {
	assert := assert.New(t)

	subnetID := ids.ID{0, 1}

	rand.Seed(0)
	chainID0 := ids.ID{}
	_, _ = rand.Read(chainID0[:]) // #nosec G404
	chainID1 := ids.ID{}
	_, _ = rand.Read(chainID1[:]) // #nosec G404

	validatorIDs := make([]ids.ShortID, MaxWindows)
	for i := range validatorIDs {
		validatorIDs[i] = ids.ShortID{byte(i + 1)}
	}
	vdrState := &validators.TestState{
		T: t,
		GetValidatorSetF: func(height uint64, subnetID ids.ID) (map[ids.ShortID]uint64, error) {
			validators := make(map[ids.ShortID]uint64, MaxWindows)
			for _, id := range validatorIDs {
				validators[id] = 1
			}
			return validators, nil
		},
	}

	w0 := New(vdrState, subnetID, chainID0)
	w1 := New(vdrState, subnetID, chainID1)

	expectedDelays0 := []time.Duration{
		5 * WindowDuration,
		2 * WindowDuration,
		0 * WindowDuration,
		3 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays0 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w0.Delay(1, 0, vdrID)
		assert.NoError(err)
		assert.EqualValues(expectedDelay, validatorDelay)
	}

	expectedDelays1 := []time.Duration{
		0 * WindowDuration,
		1 * WindowDuration,
		4 * WindowDuration,
		5 * WindowDuration,
		3 * WindowDuration,
		2 * WindowDuration,
	}
	for i, expectedDelay := range expectedDelays1 {
		vdrID := validatorIDs[i]
		validatorDelay, err := w1.Delay(1, 0, vdrID)
		assert.NoError(err)
		assert.EqualValues(expectedDelay, validatorDelay)
	}
}
