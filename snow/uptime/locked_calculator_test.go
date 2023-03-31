// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"sync"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
)

func TestLockedCalculator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	lc := NewLockedCalculator()
	require.NotNil(t)

	// Should still error because ctx is nil
	nodeID := ids.GenerateTestNodeID()
	subnetID := ids.GenerateTestID()
	_, _, err := lc.CalculateUptime(nodeID, subnetID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercent(nodeID, subnetID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercentFrom(nodeID, subnetID, time.Now())
	require.ErrorIs(err, errStillBootstrapping)

	var isBootstrapped utils.Atomic[bool]
	mockCalc := NewMockCalculator(ctrl)

	// Should still error because ctx is not bootstrapped
	lc.SetCalculator(&isBootstrapped, &sync.Mutex{}, mockCalc)
	_, _, err = lc.CalculateUptime(nodeID, subnetID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercent(nodeID, subnetID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercentFrom(nodeID, subnetID, time.Now())
	require.EqualValues(errStillBootstrapping, err)

	isBootstrapped.Set(true)

	// Should return the value from the mocked inner calculator
	mockCalc.EXPECT().CalculateUptime(gomock.Any(), gomock.Any()).AnyTimes().Return(time.Duration(0), time.Time{}, errTest)
	_, _, err = lc.CalculateUptime(nodeID, subnetID)
	require.ErrorIs(err, errTest)

	mockCalc.EXPECT().CalculateUptimePercent(gomock.Any(), gomock.Any()).AnyTimes().Return(float64(0), errTest)
	_, err = lc.CalculateUptimePercent(nodeID, subnetID)
	require.ErrorIs(err, errTest)

	mockCalc.EXPECT().CalculateUptimePercentFrom(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Return(float64(0), errTest)
	_, err = lc.CalculateUptimePercentFrom(nodeID, subnetID, time.Now())
	require.ErrorIs(err, errTest)
}
