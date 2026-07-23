// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package relayer

import (
	"log"
	"math/big"
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"
)

// --- Teleporter ABI types (mirror the Solidity struct field order) ---

type TeleporterReceipt struct {
	ReceivedMessageNonce *big.Int
	RelayerRewardAddress common.Address
}

type TeleporterMessageV2 struct {
	MessageNonce            *big.Int
	OriginSenderAddress     common.Address
	OriginTeleporterAddress common.Address
	DestinationBlockchainID [32]byte
	DestinationAddress      common.Address
	RequiredGasLimit        *big.Int
	AllowedRelayerAddresses []common.Address
	Receipts                []TeleporterReceipt
	Message                 []byte
}

type TeleporterFeeInfo struct {
	FeeTokenAddress common.Address
	Amount          *big.Int
}

type TeleporterICMMessage struct {
	Message            TeleporterMessageV2
	SourceNetworkID    uint32
	SourceBlockchainID [32]byte
	Attestation        []byte
}

// MessageDecoderABI decodes a bare abi.encode(TeleporterMessageV2) into the Go
// struct. A function with the tuple as its single OUTPUT lets us use
// ABI.UnpackIntoInterface (which abi.Arguments lacks in this libevm version).
var MessageDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"m","type":"tuple","components":[` +
		`{"name":"messageNonce","type":"uint256"},` +
		`{"name":"originSenderAddress","type":"address"},` +
		`{"name":"originTeleporterAddress","type":"address"},` +
		`{"name":"destinationBlockchainID","type":"bytes32"},` +
		`{"name":"destinationAddress","type":"address"},` +
		`{"name":"requiredGasLimit","type":"uint256"},` +
		`{"name":"allowedRelayerAddresses","type":"address[]"},` +
		`{"name":"receipts","type":"tuple[]","components":[{"name":"receivedMessageNonce","type":"uint256"},{"name":"relayerRewardAddress","type":"address"}]},` +
		`{"name":"message","type":"bytes"}]}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build decoder ABI: %v", err)
	}
	return parsed
}()

// EventDecoderABI decodes the non-indexed data of the Teleporter's
// SendCrossChainMessage event: (TeleporterMessageV2 message, TeleporterFeeInfo feeInfo).
// A function with the tuples as OUTPUTS lets us use ABI.UnpackIntoInterface.
var EventDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"m","type":"tuple","components":[` +
		`{"name":"messageNonce","type":"uint256"},` +
		`{"name":"originSenderAddress","type":"address"},` +
		`{"name":"originTeleporterAddress","type":"address"},` +
		`{"name":"destinationBlockchainID","type":"bytes32"},` +
		`{"name":"destinationAddress","type":"address"},` +
		`{"name":"requiredGasLimit","type":"uint256"},` +
		`{"name":"allowedRelayerAddresses","type":"address[]"},` +
		`{"name":"receipts","type":"tuple[]","components":[{"name":"receivedMessageNonce","type":"uint256"},{"name":"relayerRewardAddress","type":"address"}]},` +
		`{"name":"message","type":"bytes"}]},` +
		`{"name":"f","type":"tuple","components":[{"name":"feeTokenAddress","type":"address"},{"name":"amount","type":"uint256"}]}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build event decoder ABI: %v", err)
	}
	return parsed
}()

// BytesDecoderABI decodes the SendWarpMessage event data: a single `bytes`
// argument holding the unsigned warp message.
var BytesDecoderABI = func() abi.ABI {
	const j = `[{"type":"function","name":"d","inputs":[],"outputs":[{"name":"b","type":"bytes"}]}]`
	parsed, err := abi.JSON(strings.NewReader(j))
	if err != nil {
		log.Fatalf("build bytes decoder ABI: %v", err)
	}
	return parsed
}()

var (
	SendWarpMessageTopic       = crypto.Keccak256Hash([]byte("SendWarpMessage(address,bytes32,bytes)"))
	SendCrossChainMessageTopic = crypto.Keccak256Hash([]byte(
		"SendCrossChainMessage(bytes32,bytes32,(uint256,address,address,bytes32,address,uint256,address[],(uint256,address)[],bytes),(address,uint256))"))
)

// TeleporterMessageArgs returns the abi.Arguments for packing a single
// TeleporterMessageV2 tuple (the outbox log data format).
func TeleporterMessageArgs() abi.Arguments {
	t, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "messageNonce", Type: "uint256"},
		{Name: "originSenderAddress", Type: "address"},
		{Name: "originTeleporterAddress", Type: "address"},
		{Name: "destinationBlockchainID", Type: "bytes32"},
		{Name: "destinationAddress", Type: "address"},
		{Name: "requiredGasLimit", Type: "uint256"},
		{Name: "allowedRelayerAddresses", Type: "address[]"},
		{Name: "receipts", Type: "tuple[]", Components: []abi.ArgumentMarshaling{
			{Name: "receivedMessageNonce", Type: "uint256"},
			{Name: "relayerRewardAddress", Type: "address"},
		}},
		{Name: "message", Type: "bytes"},
	})
	if err != nil {
		log.Fatalf("teleporter abi type: %v", err)
	}
	return abi.Arguments{{Type: t}}
}
