package main

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

// TestDecodeTeleporterMessage confirms the relayer's decoderABI recovers a
// TeleporterMessageV2 from a bare abi.encode(TeleporterMessageV2) — the outbox
// log data — so the receiveCrossChainMessage calldata is rebuilt faithfully.
func TestDecodeTeleporterMessage(t *testing.T) {
	orig := teleporterMessageV2{
		MessageNonce:            big.NewInt(7),
		OriginSenderAddress:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		OriginTeleporterAddress: common.HexToAddress("0x2222222222222222222222222222222222222222"),
		DestinationBlockchainID: [32]byte{3, 3, 3},
		DestinationAddress:      common.HexToAddress("0x4444444444444444444444444444444444444444"),
		RequiredGasLimit:        big.NewInt(300_000),
		AllowedRelayerAddresses: []common.Address{},
		Receipts:                []teleporterReceipt{},
		Message:                 []byte("hello teleporter"),
	}
	encoded, err := teleporterMessageArgs().Pack(orig)
	require.NoError(t, err)

	var wrap struct{ M teleporterMessageV2 }
	require.NoError(t, decoderABI.UnpackIntoInterface(&wrap, "d", encoded))
	got := wrap.M

	require.Equal(t, orig.MessageNonce, got.MessageNonce)
	require.Equal(t, orig.OriginSenderAddress, got.OriginSenderAddress)
	require.Equal(t, orig.OriginTeleporterAddress, got.OriginTeleporterAddress)
	require.Equal(t, orig.DestinationBlockchainID, got.DestinationBlockchainID)
	require.Equal(t, orig.DestinationAddress, got.DestinationAddress)
	require.Equal(t, orig.RequiredGasLimit, got.RequiredGasLimit)
	require.Equal(t, orig.Message, got.Message)
	require.Empty(t, got.AllowedRelayerAddresses)
	require.Empty(t, got.Receipts)
}
