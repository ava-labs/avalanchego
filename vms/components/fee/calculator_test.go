// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	testDynamicConfig = DynamicConfig{
		GasPrice: 10,
		FeeDimensionWeights: Dimensions{
			Bandwidth: 6,
			DBRead:    10,
			DBWrite:   10,
			Compute:   1,
		},
	}
	testGasCap = Gas(math.MaxUint64)
)

func TestAddAndRemoveFees(t *testing.T) {
	require := require.New(t)

	var (
		fc = NewCalculator(testDynamicConfig.FeeDimensionWeights, testDynamicConfig.GasPrice, testGasCap)

		complexity      = Dimensions{1, 2, 3, 4}
		extraComplexity = Dimensions{2, 3, 4, 5}
		overComplexity  = Dimensions{math.MaxUint64, math.MaxUint64, math.MaxUint64, math.MaxUint64}
	)

	fee0, err := fc.GetLatestTxFee()
	require.NoError(err)
	require.Zero(fee0)
	require.NoError(fc.DoneWithLatestTx())
	gas, err := fc.GetBlockGas()
	require.NoError(err)
	require.Zero(gas)

	fee1, err := fc.AddFeesFor(complexity)
	require.NoError(err)
	require.Greater(fee1, fee0)
	gas, err = fc.GetBlockGas()
	require.NoError(err)
	require.NotZero(gas)

	// complexity can't overflow
	_, err = fc.AddFeesFor(overComplexity)
	require.ErrorIs(err, safemath.ErrOverflow)
	gas, err = fc.GetBlockGas()
	require.NoError(err)
	require.NotZero(gas)

	// can't remove more complexity than it was added
	_, err = fc.RemoveFeesFor(extraComplexity)
	require.ErrorIs(err, safemath.ErrUnderflow)
	gas, err = fc.GetBlockGas()
	require.NoError(err)
	require.NotZero(gas)

	rFee, err := fc.RemoveFeesFor(complexity)
	require.NoError(err)
	require.Equal(rFee, fee0)
	gas, err = fc.GetBlockGas()
	require.NoError(err)
	require.Zero(gas)
}

func TestGasCap(t *testing.T) {
	require := require.New(t)

	var (
		now           = time.Now().Truncate(time.Second)
		parentBlkTime = now
		// childBlkTime  = parentBlkTime.Add(time.Second)
		// grandChildBlkTime = childBlkTime.Add(5 * time.Second)

		cfg = DynamicConfig{
			MaxGasPerSecond: Gas(1_000),
			LeakGasCoeff:    Gas(5),
		}

		currCap = cfg.MaxGasPerSecond
	)

	// A block whose gas matches cap, will consume full available cap
	blkGas := cfg.MaxGasPerSecond
	currCap = UpdateGasCap(currCap, blkGas)
	require.Zero(currCap)

	// capacity grows linearly in time till MaxGas
	for i := 1; i <= 5; i++ {
		childBlkTime := parentBlkTime.Add(time.Duration(i) * time.Second)
		nextCap, err := GasCap(cfg, currCap, parentBlkTime, childBlkTime)
		require.NoError(err)
		require.Equal(Gas(i)*cfg.MaxGasPerSecond/cfg.LeakGasCoeff, nextCap)
	}

	// capacity won't grow beyond MaxGas
	childBlkTime := parentBlkTime.Add(time.Duration(6) * time.Second)
	nextCap, err := GasCap(cfg, currCap, parentBlkTime, childBlkTime)
	require.NoError(err)
	require.Equal(cfg.MaxGasPerSecond, nextCap)

	// Arrival of a block will reduce GasCap of block Gas content
	blkGas = cfg.MaxGasPerSecond / 4
	currCap = UpdateGasCap(nextCap, blkGas)
	require.Equal(3*cfg.MaxGasPerSecond/4, currCap)

	// capacity keeps growing again in time after block
	childBlkTime = parentBlkTime.Add(time.Second)
	nextCap, err = GasCap(cfg, currCap, parentBlkTime, childBlkTime)
	require.NoError(err)
	require.Equal(currCap+cfg.MaxGasPerSecond/cfg.LeakGasCoeff, nextCap)

	// time can only grow forward with capacity
	childBlkTime = parentBlkTime.Add(-1 * time.Second)
	_, err = GasCap(cfg, currCap, parentBlkTime, childBlkTime)
	require.ErrorIs(err, errChildTimeBeforeParent)
}
