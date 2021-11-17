// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime/mocks"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestLockedCalculator(t *testing.T) {
	assert := assert.New(t)
	lc := NewLockedCalculator()
	assert.NotNil(t)

	// Should still error because ctx is nil
	nodeID := ids.GenerateTestShortID()
	_, _, err := lc.CalculateUptime(nodeID)
	assert.EqualValues(errNotReady, err)
	_, err = lc.CalculateUptimePercent(nodeID)
	assert.EqualValues(errNotReady, err)
	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	assert.EqualValues(errNotReady, err)

	var isBootstrapped utils.AtomicBool
	mockCalc := &mocks.Calculator{}

	// Should still error because ctx is not bootstrapped
	lc.SetCalculator(&isBootstrapped, &sync.Mutex{}, mockCalc)
	_, _, err = lc.CalculateUptime(nodeID)
	assert.EqualValues(errNotReady, err)
	_, err = lc.CalculateUptimePercent(nodeID)
	assert.EqualValues(errNotReady, err)
	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	assert.EqualValues(errNotReady, err)

	isBootstrapped.SetValue(true)

	// Should return the value from the mocked inner calculator
	mockErr := errors.New("mock error")
	mockCalc.On("CalculateUptime", mock.Anything).Return(time.Duration(0), time.Time{}, mockErr)
	_, _, err = lc.CalculateUptime(nodeID)
	assert.EqualValues(mockErr, err)
	mockCalc.On("CalculateUptimePercent", mock.Anything).Return(float64(0), mockErr)
	_, err = lc.CalculateUptimePercent(nodeID)
	assert.EqualValues(mockErr, err)
	mockCalc.On("CalculateUptimePercentFrom", mock.Anything, mock.Anything).Return(float64(0), mockErr)
	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	assert.EqualValues(mockErr, err)
}
