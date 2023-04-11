// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"math"
	"math/rand"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/require"
)

func TestValidatorVersionsMarshalling(t *testing.T) {
	require := require.New(t)

	var (
		nodeID   = ids.GenerateTestNodeID()
		start    = rand.Uint64()                             // #nosec G404
		end      = rand.Uint64()                             // #nosec G404
		weight   = rand.Uint64()                             // #nosec G404
		duration = int64(math.Max(float64(rand.Int63()), 0)) // #nosec G404

		preContinuousVal = &Validator{
			NodeID: nodeID,
			Start:  start,
			End:    end,
			Wght:   weight,
		}

		continuousVal = &Validator{
			NodeID:          nodeID,
			StakingDuration: duration,
			Wght:            weight,
		}
	)

	// pre continuous staking checks
	preContinuousValBytes, err := Codec.Marshal(Version, preContinuousVal)
	require.NoError(err)

	retrievedVal := &Validator{}
	retrievedVersion, err := Codec.Unmarshal(preContinuousValBytes, retrievedVal)
	require.NoError(err)
	require.Equal(uint16(Version), retrievedVersion)
	require.Equal(preContinuousVal, retrievedVal)

	// post continuous staking checks
	continuousValBytes, err := Codec.Marshal(ContinuousStakingVersion, continuousVal)
	require.NoError(err)

	retrievedVal = &Validator{}
	retrievedVersion, err = Codec.Unmarshal(continuousValBytes, retrievedVal)
	require.NoError(err)
	require.Equal(uint16(ContinuousStakingVersion), retrievedVersion)
	require.Equal(continuousVal, retrievedVal)

	// regression
	regressionValBytes, err := Codec.Marshal(Version, continuousVal)
	require.NoError(err)

	retrievedVal = &Validator{}
	retrievedVersion, err = Codec.Unmarshal(regressionValBytes, retrievedVal)
	require.NoError(err)
	require.Equal(uint16(Version), retrievedVersion)
	require.Equal(&Validator{
		NodeID:          nodeID,
		Start:           0,
		End:             0,
		StakingDuration: 0,
		Wght:            weight,
	}, retrievedVal)
}
