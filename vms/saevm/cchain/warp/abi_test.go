// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestContractAddressMatchesCoreth(t *testing.T) {
	require.Equal(t, corethwarp.ContractAddress, ContractAddress, "ContractAddress")
}

func TestPackRoundTrips(t *testing.T) {
	t.Run("getVerifiedWarpMessage", func(t *testing.T) {
		in, err := PackGetVerifiedWarpMessage(7)
		require.NoError(t, err, "PackGetVerifiedWarpMessage()")
		idx, err := unpackGetVerifiedWarpMessageInput(in[4:])
		require.NoError(t, err, "unpackGetVerifiedWarpMessageInput()")
		require.Equal(t, uint32(7), idx, "unpackGetVerifiedWarpMessageInput()")

		want := GetVerifiedWarpMessageOutput{
			Message: WarpMessage{
				SourceChainID:       common.Hash{1},
				OriginSenderAddress: common.Address{2},
				Payload:             []byte{3, 4},
			},
			Valid: true,
		}
		out, err := PackGetVerifiedWarpMessageOutput(want)
		require.NoError(t, err, "PackGetVerifiedWarpMessageOutput()")
		got, err := UnpackGetVerifiedWarpMessageOutput(out)
		require.NoError(t, err, "UnpackGetVerifiedWarpMessageOutput()")
		require.Equal(t, want, got, "UnpackGetVerifiedWarpMessageOutput()")
	})

	t.Run("getVerifiedWarpBlockHash", func(t *testing.T) {
		in, err := PackGetVerifiedWarpBlockHash(3)
		require.NoError(t, err, "PackGetVerifiedWarpBlockHash()")
		idx, err := unpackGetVerifiedWarpBlockHashInput(in[4:])
		require.NoError(t, err, "unpackGetVerifiedWarpBlockHashInput()")
		require.Equal(t, uint32(3), idx, "unpackGetVerifiedWarpBlockHashInput()")

		want := GetVerifiedWarpBlockHashOutput{
			WarpBlockHash: WarpBlockHash{SourceChainID: common.Hash{5}, BlockHash: common.Hash{6}},
			Valid:         true,
		}
		out, err := PackGetVerifiedWarpBlockHashOutput(want)
		require.NoError(t, err, "PackGetVerifiedWarpBlockHashOutput()")
		got, err := UnpackGetVerifiedWarpBlockHashOutput(out)
		require.NoError(t, err, "UnpackGetVerifiedWarpBlockHashOutput()")
		require.Equal(t, want, got, "UnpackGetVerifiedWarpBlockHashOutput()")
	})

	t.Run("sendWarpMessage", func(t *testing.T) {
		in, err := PackSendWarpMessage([]byte("hello"))
		require.NoError(t, err, "PackSendWarpMessage()")
		data, err := unpackSendWarpMessageInput(in[4:])
		require.NoError(t, err, "unpackSendWarpMessageInput()")
		require.Equal(t, []byte("hello"), data, "unpackSendWarpMessageInput()")

		out, err := PackSendWarpMessageOutput(common.Hash{9})
		require.NoError(t, err, "PackSendWarpMessageOutput()")
		id, err := UnpackSendWarpMessageOutput(out)
		require.NoError(t, err, "UnpackSendWarpMessageOutput()")
		require.Equal(t, common.Hash{9}, id, "UnpackSendWarpMessageOutput()")
	})

	t.Run("SendWarpMessage_event", func(t *testing.T) {
		p, err := payload.NewAddressedCall([]byte{1}, []byte{2})
		require.NoError(t, err, "payload.NewAddressedCall()")
		msg, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, ids.GenerateTestID(), p.Bytes())
		require.NoError(t, err, "avalanchewarp.NewUnsignedMessage()")

		_, data, err := PackSendWarpMessageEvent(common.Address{1}, common.Hash(msg.ID()), msg.Bytes())
		require.NoError(t, err, "PackSendWarpMessageEvent()")
		got, err := UnpackSendWarpEventDataToMessage(data)
		require.NoError(t, err, "UnpackSendWarpEventDataToMessage()")
		require.Equal(t, msg.Bytes(), got.Bytes(), "UnpackSendWarpEventDataToMessage()")
	})
}

// TestPackMatchesCoreth pins byte equality with coreth's packers. It is the
// only reason this package's tests import coreth's warp precompile.
func TestPackMatchesCoreth(t *testing.T) {
	must := func(b []byte, err error) []byte {
		t.Helper()
		require.NoError(t, err)
		return b
	}

	require.Equal(t, must(corethwarp.PackGetBlockchainID()), must(PackGetBlockchainID()), "PackGetBlockchainID()")
	require.Equal(t, must(corethwarp.PackGetBlockchainIDOutput(common.Hash{1})), must(PackGetBlockchainIDOutput(common.Hash{1})), "PackGetBlockchainIDOutput()")
	require.Equal(t, must(corethwarp.PackGetVerifiedWarpMessage(2)), must(PackGetVerifiedWarpMessage(2)), "PackGetVerifiedWarpMessage()")
	require.Equal(t, must(corethwarp.PackGetVerifiedWarpBlockHash(2)), must(PackGetVerifiedWarpBlockHash(2)), "PackGetVerifiedWarpBlockHash()")
	require.Equal(t, must(corethwarp.PackSendWarpMessage([]byte{3})), must(PackSendWarpMessage([]byte{3})), "PackSendWarpMessage()")
	require.Equal(t, must(corethwarp.PackSendWarpMessageOutput(common.Hash{4})), must(PackSendWarpMessageOutput(common.Hash{4})), "PackSendWarpMessageOutput()")

	require.Equal(t,
		must(corethwarp.PackGetVerifiedWarpMessageOutput(corethwarp.GetVerifiedWarpMessageOutput{
			Message: corethwarp.WarpMessage{SourceChainID: common.Hash{1}, OriginSenderAddress: common.Address{2}, Payload: []byte{3}},
			Valid:   true,
		})),
		must(PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
			Message: WarpMessage{SourceChainID: common.Hash{1}, OriginSenderAddress: common.Address{2}, Payload: []byte{3}},
			Valid:   true,
		})),
		"PackGetVerifiedWarpMessageOutput()",
	)
	require.Equal(t,
		must(corethwarp.PackGetVerifiedWarpBlockHashOutput(corethwarp.GetVerifiedWarpBlockHashOutput{
			WarpBlockHash: corethwarp.WarpBlockHash{SourceChainID: common.Hash{1}, BlockHash: common.Hash{2}},
			Valid:         true,
		})),
		must(PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
			WarpBlockHash: WarpBlockHash{SourceChainID: common.Hash{1}, BlockHash: common.Hash{2}},
			Valid:         true,
		})),
		"PackGetVerifiedWarpBlockHashOutput()",
	)

	wantTopics, wantData, err := corethwarp.PackSendWarpMessageEvent(common.Address{1}, common.Hash{2}, []byte{3})
	require.NoError(t, err, "corethwarp.PackSendWarpMessageEvent()")
	gotTopics, gotData, err := PackSendWarpMessageEvent(common.Address{1}, common.Hash{2}, []byte{3})
	require.NoError(t, err, "PackSendWarpMessageEvent()")
	require.Equal(t, wantTopics, gotTopics, "PackSendWarpMessageEvent() topics")
	require.Equal(t, wantData, gotData, "PackSendWarpMessageEvent() data")
}
