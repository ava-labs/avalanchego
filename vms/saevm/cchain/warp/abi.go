// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"strings"

	"github.com/ava-labs/libevm/accounts/abi"
	"github.com/ava-labs/libevm/common"

	_ "embed"

	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// ContractAddress is the address of the warp precompile.
var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000005")

//go:embed IWarpMessenger.abi
var rawABI string

// ABI is the parsed IWarpMessenger interface.
var ABI = mustParseABI(rawABI)

func mustParseABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

// WarpBlockHash mirrors the Solidity struct of the same name.
type WarpBlockHash struct {
	SourceChainID common.Hash
	BlockHash     common.Hash
}

// GetVerifiedWarpBlockHashOutput is the return value of getVerifiedWarpBlockHash.
type GetVerifiedWarpBlockHashOutput struct {
	WarpBlockHash WarpBlockHash
	Valid         bool
}

// WarpMessage mirrors the Solidity struct of the same name.
type WarpMessage struct {
	SourceChainID       common.Hash
	OriginSenderAddress common.Address
	Payload             []byte
}

// GetVerifiedWarpMessageOutput is the return value of getVerifiedWarpMessage.
type GetVerifiedWarpMessageOutput struct {
	Message WarpMessage
	Valid   bool
}

type sendWarpMessageEventData struct {
	Message []byte
}

// PackGetBlockchainID packs the call data, including the selector.
func PackGetBlockchainID() ([]byte, error) {
	return ABI.Pack("getBlockchainID")
}

// PackGetBlockchainIDOutput packs the return value of getBlockchainID.
func PackGetBlockchainIDOutput(blockchainID common.Hash) ([]byte, error) {
	return ABI.PackOutput("getBlockchainID", blockchainID)
}

// unpackGetVerifiedWarpBlockHashInput unpacks the arguments (without the
// selector) of getVerifiedWarpBlockHash. Strict mode is off, as it has been
// since Durango.
func unpackGetVerifiedWarpBlockHashInput(input []byte) (uint32, error) {
	var index uint32
	if err := ABI.UnpackInputIntoInterface(&index, "getVerifiedWarpBlockHash", input); err != nil {
		return 0, err
	}
	return index, nil
}

// PackGetVerifiedWarpBlockHash packs the call data, including the selector.
func PackGetVerifiedWarpBlockHash(index uint32) ([]byte, error) {
	return ABI.Pack("getVerifiedWarpBlockHash", index)
}

// PackGetVerifiedWarpBlockHashOutput packs the return value of getVerifiedWarpBlockHash.
func PackGetVerifiedWarpBlockHashOutput(out GetVerifiedWarpBlockHashOutput) ([]byte, error) {
	return ABI.PackOutput("getVerifiedWarpBlockHash", out.WarpBlockHash, out.Valid)
}

// UnpackGetVerifiedWarpBlockHashOutput unpacks the return value of getVerifiedWarpBlockHash.
func UnpackGetVerifiedWarpBlockHashOutput(output []byte) (GetVerifiedWarpBlockHashOutput, error) {
	var out GetVerifiedWarpBlockHashOutput
	err := ABI.UnpackIntoInterface(&out, "getVerifiedWarpBlockHash", output)
	return out, err
}

// unpackGetVerifiedWarpMessageInput unpacks the arguments (without the
// selector) of getVerifiedWarpMessage. Strict mode is off, as it has been
// since Durango.
func unpackGetVerifiedWarpMessageInput(input []byte) (uint32, error) {
	var index uint32
	if err := ABI.UnpackInputIntoInterface(&index, "getVerifiedWarpMessage", input); err != nil {
		return 0, err
	}
	return index, nil
}

// PackGetVerifiedWarpMessage packs the call data, including the selector.
func PackGetVerifiedWarpMessage(index uint32) ([]byte, error) {
	return ABI.Pack("getVerifiedWarpMessage", index)
}

// PackGetVerifiedWarpMessageOutput packs the return value of getVerifiedWarpMessage.
func PackGetVerifiedWarpMessageOutput(out GetVerifiedWarpMessageOutput) ([]byte, error) {
	return ABI.PackOutput("getVerifiedWarpMessage", out.Message, out.Valid)
}

// UnpackGetVerifiedWarpMessageOutput unpacks the return value of getVerifiedWarpMessage.
func UnpackGetVerifiedWarpMessageOutput(output []byte) (GetVerifiedWarpMessageOutput, error) {
	var out GetVerifiedWarpMessageOutput
	err := ABI.UnpackIntoInterface(&out, "getVerifiedWarpMessage", output)
	return out, err
}

// unpackSendWarpMessageInput unpacks the arguments (without the selector) of
// sendWarpMessage. Strict mode is off, as it has been since Durango.
func unpackSendWarpMessageInput(input []byte) ([]byte, error) {
	var payloadData []byte
	if err := ABI.UnpackInputIntoInterface(&payloadData, "sendWarpMessage", input); err != nil {
		return nil, err
	}
	return payloadData, nil
}

// PackSendWarpMessage packs the call data, including the selector.
func PackSendWarpMessage(payloadData []byte) ([]byte, error) {
	return ABI.Pack("sendWarpMessage", payloadData)
}

// PackSendWarpMessageOutput packs the return value of sendWarpMessage.
func PackSendWarpMessageOutput(messageID common.Hash) ([]byte, error) {
	return ABI.PackOutput("sendWarpMessage", messageID)
}

// UnpackSendWarpMessageOutput unpacks the return value of sendWarpMessage.
func UnpackSendWarpMessageOutput(output []byte) (common.Hash, error) {
	res, err := ABI.Unpack("sendWarpMessage", output)
	if err != nil {
		return common.Hash{}, err
	}
	return *abi.ConvertType(res[0], new(common.Hash)).(*common.Hash), nil
}

// PackSendWarpMessageEvent packs the topics and data of a SendWarpMessage event.
func PackSendWarpMessageEvent(sourceAddress common.Address, unsignedMessageID common.Hash, unsignedMessageBytes []byte) ([]common.Hash, []byte, error) {
	return ABI.PackEvent("SendWarpMessage", sourceAddress, unsignedMessageID, unsignedMessageBytes)
}

// UnpackSendWarpEventDataToMessage parses the data of a SendWarpMessage event
// into the unsigned message it carries.
func UnpackSendWarpEventDataToMessage(data []byte) (*avalanchewarp.UnsignedMessage, error) {
	var event sendWarpMessageEventData
	if err := ABI.UnpackIntoInterface(&event, "SendWarpMessage", data); err != nil {
		return nil, err
	}
	return avalanchewarp.ParseUnsignedMessage(event.Message)
}
