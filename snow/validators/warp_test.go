// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators_test

import (
	"encoding/hex"
	"encoding/json"
	"math"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"

	safemath "github.com/ava-labs/avalanchego/utils/math"

	. "github.com/ava-labs/avalanchego/snow/validators"
)

func TestWarpJSON(t *testing.T) {
	const pkStr = "88c4760201a451619475ff7d3782d02381826bad5bef306d1ff6b22d3fb2e137bc004d867054efc241463fe4b21c61af"
	pkBytes, err := hex.DecodeString(pkStr)
	require.NoError(t, err)
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(t, err)

	w := &Warp{
		PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
		Weight:         12345,
		NodeIDs: []ids.NodeID{
			{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
			{0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
		},
	}
	wJSON, err := json.MarshalIndent(w, "", "\t")
	require.NoError(t, err)

	const expectedJSON = `{
	"publicKey": "0x88c4760201a451619475ff7d3782d02381826bad5bef306d1ff6b22d3fb2e137bc004d867054efc241463fe4b21c61af",
	"weight": "12345",
	"nodeIDs": [
		"NodeID-12D2adLM3UJyWgFWD2VXv5vkMT8MoMbg",
		"NodeID-jVEbJZmHxdPnxBthD7v8dC96qyUrPMxg"
	]
}`
	require.JSONEq(t, expectedJSON, string(wJSON))

	var parsedW Warp
	require.NoError(t, json.Unmarshal(wJSON, &parsedW))

	// Compare only public fields, not cache
	require.Equal(t, w.PublicKeyBytes, parsedW.PublicKeyBytes)
	require.Equal(t, w.Weight, parsedW.Weight)
	require.Equal(t, w.NodeIDs, parsedW.NodeIDs)
}

func TestWarpSetJSON(t *testing.T) {
	const pkStr = "88c4760201a451619475ff7d3782d02381826bad5bef306d1ff6b22d3fb2e137bc004d867054efc241463fe4b21c61af"
	pkBytes, err := hex.DecodeString(pkStr)
	require.NoError(t, err)
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(t, err)

	ws := WarpSet{
		Validators: []*Warp{
			{
				PublicKeyBytes: bls.PublicKeyToUncompressedBytes(pk),
				Weight:         12345,
				NodeIDs: []ids.NodeID{
					{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07},
					{0x08, 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f},
				},
			},
		},
		TotalWeight: 54321,
	}
	wsJSON, err := json.MarshalIndent(ws, "", "\t")
	require.NoError(t, err)

	const expectedJSON = `{
	"validators": [
		{
			"publicKey": "0x88c4760201a451619475ff7d3782d02381826bad5bef306d1ff6b22d3fb2e137bc004d867054efc241463fe4b21c61af",
			"weight": "12345",
			"nodeIDs": [
				"NodeID-12D2adLM3UJyWgFWD2VXv5vkMT8MoMbg",
				"NodeID-jVEbJZmHxdPnxBthD7v8dC96qyUrPMxg"
			]
		}
	],
	"totalWeight": "54321"
}`
	require.JSONEq(t, expectedJSON, string(wsJSON))

	var parsedWS WarpSet
	require.NoError(t, json.Unmarshal(wsJSON, &parsedWS))

	// Compare only public fields, not cache
	require.Equal(t, ws.TotalWeight, parsedWS.TotalWeight)
	require.Len(t, parsedWS.Validators, len(ws.Validators))
	for i := range ws.Validators {
		require.Equal(t, ws.Validators[i].PublicKeyBytes, parsedWS.Validators[i].PublicKeyBytes)
		require.Equal(t, ws.Validators[i].Weight, parsedWS.Validators[i].Weight)
		require.Equal(t, ws.Validators[i].NodeIDs, parsedWS.Validators[i].NodeIDs)
	}
}

func TestFlattenValidatorSet(t *testing.T) {
	var (
		vdrs    = validatorstest.NewWarpSet(t, 3)
		nodeID0 = vdrs.Validators[0].NodeIDs[0]
		nodeID1 = vdrs.Validators[1].NodeIDs[0]
		nodeID2 = vdrs.Validators[2].NodeIDs[0]
	)
	tests := []struct {
		name       string
		validators map[ids.NodeID]*GetValidatorOutput
		want       WarpSet
		wantErr    error
	}{
		{
			name: "overflow",
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: vdrs.Validators[1].PublicKey(),
					Weight:    math.MaxUint64,
				},
			},
			wantErr: safemath.ErrOverflow,
		},
		{
			name: "nil_public_key_skipped",
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
				nodeID1: {
					NodeID:    nodeID1,
					PublicKey: nil,
					Weight:    5, // don't use 1 to distinguish from default
				},
			},
			want: WarpSet{
				Validators:  []*Warp{vdrs.Validators[0]},
				TotalWeight: vdrs.Validators[0].Weight + 5,
			},
		},
		{
			name: "sorted", // Would non-deterministically fail without sorting
			validators: map[ids.NodeID]*GetValidatorOutput{
				nodeID0: validatorstest.WarpToOutput(vdrs.Validators[0]),
				nodeID1: validatorstest.WarpToOutput(vdrs.Validators[1]),
				nodeID2: validatorstest.WarpToOutput(vdrs.Validators[2]),
			},
			want: vdrs,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			got, err := FlattenValidatorSet(test.validators)
			require.ErrorIs(err, test.wantErr)

			// Compare only the public fields, not cache fields
			require.Equal(test.want.TotalWeight, got.TotalWeight)
			require.Len(got.Validators, len(test.want.Validators))
			for i, wantVdr := range test.want.Validators {
				gotVdr := got.Validators[i]
				require.Equal(wantVdr.PublicKeyBytes, gotVdr.PublicKeyBytes)
				require.Equal(wantVdr.Weight, gotVdr.Weight)
				require.Equal(wantVdr.NodeIDs, gotVdr.NodeIDs)
			}
		})
	}
}

func BenchmarkFlattenValidatorSet(b *testing.B) {
	for size := 1; size <= 1<<10; size *= 2 {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			vdrs := make(
				map[ids.NodeID]*GetValidatorOutput,
				size,
			)
			for range size {
				nodeID := ids.GenerateTestNodeID()
				secretKey, err := localsigner.New()
				require.NoError(b, err)
				publicKey := secretKey.PublicKey()
				vdrs[nodeID] = &GetValidatorOutput{
					NodeID:    nodeID,
					PublicKey: publicKey,
					Weight:    1,
				}
			}
			for b.Loop() {
				_, err := FlattenValidatorSet(vdrs)
				require.NoError(b, err)
			}
		})
	}
}
