// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

//nolint:gosec // This does not need cryptographically strong random numbers
func generateUTXOs(assetID ids.ID, locktime uint64) []*avax.UTXO {
	utxos := make([]*avax.UTXO, rand.Intn(10))
	for i := range utxos {
		var output avax.TransferableOut = &secp256k1fx.TransferOutput{
			Amt: rand.Uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  rand.Uint64(),
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		}
		if locktime != 0 {
			output = &stakeable.LockOut{
				Locktime:        locktime,
				TransferableOut: output,
			}
		}
		utxos[i] = &avax.UTXO{
			UTXOID: avax.UTXOID{
				TxID:        ids.GenerateTestID(),
				OutputIndex: rand.Uint32(),
			},
			Asset: avax.Asset{
				ID: assetID,
			},
			Out: output,
		}
	}
	return utxos
}

func TestSplitUTXOsByLocktime(t *testing.T) {
	var (
		require = require.New(t)

		expectedUnlockedUTXOsPart0 = generateUTXOs(ids.GenerateTestID(), 0)
		expectedUnlockedUTXOsPart1 = generateUTXOs(ids.GenerateTestID(), 100)
		expectedLockedUTXOsPart0   = generateUTXOs(ids.GenerateTestID(), 10000)
		expectedUnlockedUTXOsPart2 = generateUTXOs(ids.GenerateTestID(), 0)
		expectedLockedUTXOsPart1   = generateUTXOs(ids.GenerateTestID(), 101)

		utxos = utils.Join(
			expectedUnlockedUTXOsPart0,
			expectedUnlockedUTXOsPart1,
			expectedLockedUTXOsPart0,
			expectedUnlockedUTXOsPart2,
			expectedLockedUTXOsPart1,
		)
		expectedUnlockedUTXOs = utils.Join(
			expectedUnlockedUTXOsPart0,
			expectedUnlockedUTXOsPart1,
			expectedUnlockedUTXOsPart2,
		)
		expectedLockedUTXOs = utils.Join(
			expectedLockedUTXOsPart0,
			expectedLockedUTXOsPart1,
		)
	)

	utxosWithAssetID, utxosWithOtherAssetID := splitUTXOsByLocktime(utxos, 100)
	require.Equal(expectedUnlockedUTXOs, utxosWithAssetID)
	require.Equal(expectedLockedUTXOs, utxosWithOtherAssetID)
}

func TestSplitUTXOsByAssetID(t *testing.T) {
	var (
		require = require.New(t)

		assetID                            = ids.GenerateTestID()
		expectedUTXOsWithAssetIDPart0      = generateUTXOs(assetID, 0)
		expectedUTXOsWithAssetIDPart1      = generateUTXOs(assetID, 100)
		expectedUTXOsWithAssetIDPart2      = generateUTXOs(assetID, 10000)
		expectedUTXOsWithOtherAssetIDPart0 = generateUTXOs(ids.GenerateTestID(), 0)
		expectedUTXOsWithOtherAssetIDPart1 = generateUTXOs(ids.GenerateTestID(), 100)

		utxos = utils.Join(
			expectedUTXOsWithAssetIDPart0,
			expectedUTXOsWithOtherAssetIDPart0,
			expectedUTXOsWithAssetIDPart1,
			expectedUTXOsWithOtherAssetIDPart1,
			expectedUTXOsWithAssetIDPart2,
		)
		expectedUTXOsWithAssetID = utils.Join(
			expectedUTXOsWithAssetIDPart0,
			expectedUTXOsWithAssetIDPart1,
			expectedUTXOsWithAssetIDPart2,
		)
		expectedUTXOsWithOtherAssetID = utils.Join(
			expectedUTXOsWithOtherAssetIDPart0,
			expectedUTXOsWithOtherAssetIDPart1,
		)
	)

	utxosWithAssetID, utxosWithOtherAssetID := splitUTXOsByAssetID(utxos, assetID)
	require.Equal(expectedUTXOsWithAssetID, utxosWithAssetID)
	require.Equal(expectedUTXOsWithOtherAssetID, utxosWithOtherAssetID)
}

func TestUnwrapOutput(t *testing.T) {
	normalOutput := &secp256k1fx.TransferOutput{
		Amt: 123,
		OutputOwners: secp256k1fx.OutputOwners{
			Locktime:  456,
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.ShortEmpty},
		},
	}

	tests := []struct {
		name             string
		output           verify.State
		expectedOutput   *secp256k1fx.TransferOutput
		expectedLocktime uint64
		expectedErr      error
	}{
		{
			name:             "normal output",
			output:           normalOutput,
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name: "locked output",
			output: &stakeable.LockOut{
				Locktime:        789,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 789,
			expectedErr:      nil,
		},
		{
			name: "locked output with no locktime",
			output: &stakeable.LockOut{
				Locktime:        0,
				TransferableOut: normalOutput,
			},
			expectedOutput:   normalOutput,
			expectedLocktime: 0,
			expectedErr:      nil,
		},
		{
			name:             "invalid output",
			output:           nil,
			expectedOutput:   nil,
			expectedLocktime: 0,
			expectedErr:      ErrUnknownOutputType,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			output, locktime, err := unwrapOutput(test.output)
			require.Equal(test.expectedOutput, output)
			require.Equal(test.expectedLocktime, locktime)
			require.ErrorIs(err, test.expectedErr)
		})
	}
}
