// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"math/rand"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

func generateUTXOs(random *rand.Rand, assetID ids.ID, locktime uint64) []*avax.UTXO {
	utxos := make([]*avax.UTXO, random.Intn(10))
	for i := range utxos {
		var output avax.TransferableOut = &secp256k1fx.TransferOutput{
			Amt: random.Uint64(),
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  random.Uint64(),
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
				OutputIndex: random.Uint32(),
			},
			Asset: avax.Asset{
				ID: assetID,
			},
			Out: output,
		}
	}
	return utxos
}

func TestSplitByLocktime(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("Seed: %d", seed)
	random := rand.New(rand.NewSource(seed))

	var (
		require = require.New(t)

		unlockedTime     uint64 = 100
		expectedUnlocked        = slices.Concat(
			generateUTXOs(random, ids.GenerateTestID(), 0),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime-1),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime),
		)
		expectedLocked = slices.Concat(
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime+100),
			generateUTXOs(random, ids.GenerateTestID(), unlockedTime+1),
		)
		utxos = slices.Concat(
			expectedUnlocked,
			expectedLocked,
		)
	)
	random.Shuffle(len(utxos), func(i, j int) {
		utxos[i], utxos[j] = utxos[j], utxos[i]
	})

	utxosByLocktime := splitByLocktime(utxos, unlockedTime)
	require.ElementsMatch(expectedUnlocked, utxosByLocktime.unlocked)
	require.ElementsMatch(expectedLocked, utxosByLocktime.locked)
}

func TestByAssetID(t *testing.T) {
	seed := time.Now().UnixNano()
	t.Logf("Seed: %d", seed)
	random := rand.New(rand.NewSource(seed))

	var (
		require = require.New(t)

		assetID           = ids.GenerateTestID()
		expectedRequested = generateUTXOs(random, assetID, random.Uint64())
		expectedOther     = generateUTXOs(random, ids.GenerateTestID(), random.Uint64())
		utxos             = slices.Concat(
			expectedRequested,
			expectedOther,
		)
	)
	random.Shuffle(len(utxos), func(i, j int) {
		utxos[i], utxos[j] = utxos[j], utxos[i]
	})

	utxosByAssetID := splitByAssetID(utxos, assetID)
	require.ElementsMatch(expectedRequested, utxosByAssetID.requested)
	require.ElementsMatch(expectedOther, utxosByAssetID.other)
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
			require.ErrorIs(err, test.expectedErr)
			require.Equal(test.expectedOutput, output)
			require.Equal(test.expectedLocktime, locktime)
		})
	}
}
