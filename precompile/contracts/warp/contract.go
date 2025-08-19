// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/coreth/accounts/abi"
	"github.com/ava-labs/coreth/precompile/contract"
)

const (
	GetVerifiedWarpMessageBaseCost uint64 = 2      // Base cost of entering getVerifiedWarpMessage
	GetBlockchainIDGasCost         uint64 = 2      // Based on GasQuickStep used in existing EVM instructions
	AddWarpMessageGasCost          uint64 = 20_000 // Cost of producing and serving a BLS Signature
	// Sum of base log gas cost, cost of producing 4 topics, and producing + serving a BLS Signature (sign + trie write)
	// Note: using trie write for the gas cost results in a conservative overestimate since the message is stored in a
	// flat database that can be cleaned up after a period of time instead of the EVM trie.

	SendWarpMessageGasCost uint64 = contract.LogGas + 3*contract.LogTopicGas + AddWarpMessageGasCost + contract.WriteGasCostPerSlot
	// SendWarpMessageGasCostPerByte cost accounts for producing a signed message of a given size
	SendWarpMessageGasCostPerByte uint64 = contract.LogDataGas

	GasCostPerWarpSigner            uint64 = 500
	GasCostPerWarpMessageBytes      uint64 = 100
	GasCostPerSignatureVerification uint64 = 200_000
)

var (
	errInvalidSendInput  = errors.New("invalid sendWarpMessage input")
	errInvalidIndexInput = errors.New("invalid index to specify warp message")
)

// Singleton StatefulPrecompiledContract and signatures.
var (
	// WarpRawABI contains the raw ABI of Warp contract.
	//go:embed contract.abi
	WarpRawABI string

	WarpABI = contract.ParseABI(WarpRawABI)

	WarpPrecompile = createWarpPrecompile()
)

// WarpBlockHash is an auto generated low-level Go binding around an user-defined struct.
type WarpBlockHash struct {
	SourceChainID common.Hash
	BlockHash     common.Hash
}

type GetVerifiedWarpBlockHashOutput struct {
	WarpBlockHash WarpBlockHash
	Valid         bool
}

// WarpMessage is an auto generated low-level Go binding around an user-defined struct.
type WarpMessage struct {
	SourceChainID       common.Hash
	OriginSenderAddress common.Address
	Payload             []byte
}

type GetVerifiedWarpMessageOutput struct {
	Message WarpMessage
	Valid   bool
}

type SendWarpMessageEventData struct {
	Message []byte
}

// PackGetBlockchainID packs the include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetBlockchainID() ([]byte, error) {
	return WarpABI.Pack("getBlockchainID")
}

// PackGetBlockchainIDOutput attempts to pack given blockchainID of type common.Hash
// to conform the ABI outputs.
func PackGetBlockchainIDOutput(blockchainID common.Hash) ([]byte, error) {
	return WarpABI.PackOutput("getBlockchainID", blockchainID)
}

// getBlockchainID returns the snow Chain Context ChainID of this blockchain.
func getBlockchainID(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, GetBlockchainIDGasCost); err != nil {
		return nil, 0, err
	}
	packedOutput, err := PackGetBlockchainIDOutput(common.Hash(accessibleState.GetSnowContext().ChainID))
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// UnpackGetVerifiedWarpBlockHashInput attempts to unpack [input] into the uint32 type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpBlockHashInput(input []byte) (uint32, error) {
	// We don't use strict mode here because it was disabled with Durango.
	// Since Warp will be deployed after Durango, we don't need to use strict mode
	res, err := WarpABI.UnpackInput("getVerifiedWarpBlockHash", input, false)
	if err != nil {
		return 0, err
	}
	unpacked := *abi.ConvertType(res[0], new(uint32)).(*uint32)
	return unpacked, nil
}

// PackGetVerifiedWarpBlockHash packs [index] of type uint32 into the appropriate arguments for getVerifiedWarpBlockHash.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetVerifiedWarpBlockHash(index uint32) ([]byte, error) {
	return WarpABI.Pack("getVerifiedWarpBlockHash", index)
}

// PackGetVerifiedWarpBlockHashOutput attempts to pack given [outputStruct] of type GetVerifiedWarpBlockHashOutput
// to conform the ABI outputs.
func PackGetVerifiedWarpBlockHashOutput(outputStruct GetVerifiedWarpBlockHashOutput) ([]byte, error) {
	return WarpABI.PackOutput("getVerifiedWarpBlockHash",
		outputStruct.WarpBlockHash,
		outputStruct.Valid,
	)
}

// UnpackGetVerifiedWarpBlockHashOutput attempts to unpack [output] as GetVerifiedWarpBlockHashOutput
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpBlockHashOutput(output []byte) (GetVerifiedWarpBlockHashOutput, error) {
	outputStruct := GetVerifiedWarpBlockHashOutput{}
	err := WarpABI.UnpackIntoInterface(&outputStruct, "getVerifiedWarpBlockHash", output)

	return outputStruct, err
}

func getVerifiedWarpBlockHash(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return handleWarpMessage(accessibleState, input, suppliedGas, blockHashHandler{})
}

// UnpackGetVerifiedWarpMessageInput attempts to unpack [input] into the uint32 type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpMessageInput(input []byte) (uint32, error) {
	// We don't use strict mode here because it was disabled with Durango.
	// Since Warp will be deployed after Durango, we don't need to use strict mode.
	res, err := WarpABI.UnpackInput("getVerifiedWarpMessage", input, false)
	if err != nil {
		return 0, err
	}
	unpacked := *abi.ConvertType(res[0], new(uint32)).(*uint32)
	return unpacked, nil
}

// PackGetVerifiedWarpMessage packs [index] of type uint32 into the appropriate arguments for getVerifiedWarpMessage.
// the packed bytes include selector (first 4 func signature bytes).
// This function is mostly used for tests.
func PackGetVerifiedWarpMessage(index uint32) ([]byte, error) {
	return WarpABI.Pack("getVerifiedWarpMessage", index)
}

// PackGetVerifiedWarpMessageOutput attempts to pack given [outputStruct] of type GetVerifiedWarpMessageOutput
// to conform the ABI outputs.
func PackGetVerifiedWarpMessageOutput(outputStruct GetVerifiedWarpMessageOutput) ([]byte, error) {
	return WarpABI.PackOutput("getVerifiedWarpMessage",
		outputStruct.Message,
		outputStruct.Valid,
	)
}

// UnpackGetVerifiedWarpMessageOutput attempts to unpack [output] as GetVerifiedWarpMessageOutput
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpMessageOutput(output []byte) (GetVerifiedWarpMessageOutput, error) {
	outputStruct := GetVerifiedWarpMessageOutput{}
	err := WarpABI.UnpackIntoInterface(&outputStruct, "getVerifiedWarpMessage", output)

	return outputStruct, err
}

// getVerifiedWarpMessage retrieves the pre-verified warp message from the predicate storage slots and returns
// the expected ABI encoding of the message to the caller.
func getVerifiedWarpMessage(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	return handleWarpMessage(accessibleState, input, suppliedGas, addressedPayloadHandler{})
}

// UnpackSendWarpMessageInput attempts to unpack [input] as []byte
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSendWarpMessageInput(input []byte) ([]byte, error) {
	// We don't use strict mode here because it was disabled with Durango.
	// Since Warp will be deployed after Durango, we don't need to use strict mode.
	res, err := WarpABI.UnpackInput("sendWarpMessage", input, false)
	if err != nil {
		return []byte{}, err
	}
	unpacked := *abi.ConvertType(res[0], new([]byte)).(*[]byte)
	return unpacked, nil
}

// PackSendWarpMessage packs [inputStruct] of type []byte into the appropriate arguments for sendWarpMessage.
func PackSendWarpMessage(payloadData []byte) ([]byte, error) {
	return WarpABI.Pack("sendWarpMessage", payloadData)
}

// PackSendWarpMessageOutput attempts to pack given messageID of type common.Hash
// to conform the ABI outputs.
func PackSendWarpMessageOutput(messageID common.Hash) ([]byte, error) {
	return WarpABI.PackOutput("sendWarpMessage", messageID)
}

// UnpackSendWarpMessageOutput attempts to unpack given [output] into the common.Hash type output
// assumes that [output] does not include selector (omits first 4 func signature bytes)
func UnpackSendWarpMessageOutput(output []byte) (common.Hash, error) {
	res, err := WarpABI.Unpack("sendWarpMessage", output)
	if err != nil {
		return common.Hash{}, err
	}
	unpacked := *abi.ConvertType(res[0], new(common.Hash)).(*common.Hash)
	return unpacked, nil
}

// sendWarpMessage constructs an Avalanche Warp Message containing an AddressedPayload and emits a log to signal validators that they should
// be willing to sign this message.
func sendWarpMessage(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	if remainingGas, err = contract.DeductGas(suppliedGas, SendWarpMessageGasCost); err != nil {
		return nil, 0, err
	}
	// This gas cost includes buffer room because it is based off of the total size of the input instead of the produced payload.
	// This ensures that we charge gas before we unpack the variable sized input.
	payloadGas, overflow := math.SafeMul(SendWarpMessageGasCostPerByte, uint64(len(input)))
	if overflow {
		return nil, 0, vm.ErrOutOfGas
	}
	if remainingGas, err = contract.DeductGas(remainingGas, payloadGas); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vm.ErrWriteProtection
	}
	// unpack the arguments
	payloadData, err := UnpackSendWarpMessageInput(input)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %w", errInvalidSendInput, err)
	}

	var (
		sourceChainID = accessibleState.GetSnowContext().ChainID
		sourceAddress = caller
	)

	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	if err != nil {
		return nil, remainingGas, err
	}
	unsignedWarpMessage, err := warp.NewUnsignedMessage(
		accessibleState.GetSnowContext().NetworkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	if err != nil {
		return nil, remainingGas, err
	}

	// Add a log to be handled if this action is finalized.
	topics, data, err := PackSendWarpMessageEvent(
		sourceAddress,
		common.Hash(unsignedWarpMessage.ID()),
		unsignedWarpMessage.Bytes(),
	)
	if err != nil {
		return nil, remainingGas, err
	}
	accessibleState.GetStateDB().AddLog(&types.Log{
		Address:     ContractAddress,
		Topics:      topics,
		Data:        data,
		BlockNumber: accessibleState.GetBlockContext().Number().Uint64(),
	})

	packed, err := PackSendWarpMessageOutput(common.Hash(unsignedWarpMessage.ID()))
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed message ID and the remaining gas
	return packed, remainingGas, nil
}

// PackSendWarpMessageEvent packs the given arguments into SendWarpMessage events including topics and data.
func PackSendWarpMessageEvent(sourceAddress common.Address, unsignedMessageID common.Hash, unsignedMessageBytes []byte) ([]common.Hash, []byte, error) {
	return WarpABI.PackEvent("SendWarpMessage", sourceAddress, unsignedMessageID, unsignedMessageBytes)
}

// UnpackSendWarpEventDataToMessage attempts to unpack event [data] as warp.UnsignedMessage.
func UnpackSendWarpEventDataToMessage(data []byte) (*warp.UnsignedMessage, error) {
	event := SendWarpMessageEventData{}
	err := WarpABI.UnpackIntoInterface(&event, "SendWarpMessage", data)
	if err != nil {
		return nil, err
	}
	return warp.ParseUnsignedMessage(event.Message)
}

// createWarpPrecompile returns a StatefulPrecompiledContract with getters and setters for the precompile.
func createWarpPrecompile() contract.StatefulPrecompiledContract {
	var functions []*contract.StatefulPrecompileFunction

	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"getBlockchainID":          getBlockchainID,
		"getVerifiedWarpBlockHash": getVerifiedWarpBlockHash,
		"getVerifiedWarpMessage":   getVerifiedWarpMessage,
		"sendWarpMessage":          sendWarpMessage,
	}

	for name, function := range abiFunctionMap {
		method, ok := WarpABI.Methods[name]
		if !ok {
			panic(fmt.Errorf("given method (%s) does not exist in the ABI", name))
		}
		functions = append(functions, contract.NewStatefulPrecompileFunction(method.ID, function))
	}
	// Construct the contract with no fallback function.
	statefulContract, err := contract.NewStatefulPrecompileContract(nil, functions)
	if err != nil {
		panic(err)
	}
	return statefulContract
}
