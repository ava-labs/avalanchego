// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptime

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime/uptimemock"
	"github.com/ava-labs/avalanchego/utils"
)

func TestLockedCalculator(t *testing.T) {
	require := require.New(t)
	ctrl := gomock.NewController(t)

	lc := NewLockedCalculator()
	require.NotNil(lc)

	// Should still error because ctx is nil
	nodeID := ids.GenerateTestNodeID()
	_, _, err := lc.CalculateUptime(nodeID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercent(nodeID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	require.ErrorIs(err, errStillBootstrapping)

	var isBootstrapped utils.Atomic[bool]
	mockCalc := uptimemock.NewCalculator(ctrl)

	// Should still error because ctx is not bootstrapped
	lc.SetCalculator(&isBootstrapped, &sync.Mutex{}, mockCalc)
	_, _, err = lc.CalculateUptime(nodeID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercent(nodeID)
	require.ErrorIs(err, errStillBootstrapping)

	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	require.ErrorIs(err, errStillBootstrapping)

	isBootstrapped.Set(true)

	// Should return the value from the mocked inner calculator
	mockCalc.EXPECT().CalculateUptime(gomock.Any()).AnyTimes().Return(time.Duration(0), time.Time{}, errTest)
	_, _, err = lc.CalculateUptime(nodeID)
	require.ErrorIs(err, errTest)

	mockCalc.EXPECT().CalculateUptimePercent(gomock.Any()).AnyTimes().Return(float64(0), errTest)
	_, err = lc.CalculateUptimePercent(nodeID)
	require.ErrorIs(err, errTest)

	mockCalc.EXPECT().CalculateUptimePercentFrom(gomock.Any(), gomock.Any()).AnyTimes().Return(float64(0), errTest)
	_, err = lc.CalculateUptimePercentFrom(nodeID, time.Now())
	require.ErrorIs(err, errTest)
}
