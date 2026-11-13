// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"

	_ "embed"

	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contract"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/message"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
)

const addWarpMessageBaseGasCost uint64 = 20_000 // Cost of producing and serving a BLS Signature

var (
	// PreGraniteGasConfig is the cost of Warp operations from Durango until
	// Granite.
	PreGraniteGasConfig = GasConfig{
		GetBlockchainID: 2,

		GetVerifiedWarpMessageBase: 2,
		PerWarpSigner:              500,
		PerWarpMessageChunk:        3_200,
		VerifyPredicateBase:        200_000,

		// Sum of base log gas cost, cost of producing 3 topics, and
		// producing + serving a BLS Signature (sign + trie write).
		//
		// Note: using trie write for the gas cost results in a conservative
		// overestimate since the message is stored in a flat database that can
		// be cleaned up after a period of time instead of the EVM trie.
		SendWarpMessageBase: contract.LogGas + 3*contract.LogTopicGas + addWarpMessageBaseGasCost + contract.WriteGasCostPerSlot,
		PerWarpMessageByte:  contract.LogDataGas,
	}
	// GraniteGasConfig updated the gas costs of warp operations to target a
	// processing speed of around 100 mgas/s after the introduction of epoching
	// which allows for pre-calculating the warp validator sets for all active
	// networks.
	GraniteGasConfig = GasConfig{
		GetBlockchainID: 200,

		GetVerifiedWarpMessageBase: 750,
		PerWarpSigner:              250,
		PerWarpMessageChunk:        512, // matches call data byte cost
		VerifyPredicateBase:        125_000,

		// Unchanged during Granite.
		SendWarpMessageBase: PreGraniteGasConfig.SendWarpMessageBase,
		PerWarpMessageByte:  PreGraniteGasConfig.PerWarpMessageByte,
	}
)

// A GasConfig is the cost of each Warp operation.
type GasConfig struct {
	// Cost to call getBlockchainID
	GetBlockchainID uint64

	// Base cost of entering getVerifiedWarpMessage
	GetVerifiedWarpMessageBase uint64
	// Gas cost per warp signer in the validator set
	PerWarpSigner uint64
	// Gas cost per chunk of the warp message (each chunk is 32 bytes)
	PerWarpMessageChunk uint64
	// Gas cost to verify a BLS signature
	VerifyPredicateBase uint64

	// Base cost of entering sendWarpMessage
	SendWarpMessageBase uint64
	// PerWarpMessageByte cost accounts for producing a message of a given size
	PerWarpMessageByte uint64
}

var (
	errInvalidSendInput  = errors.New("invalid sendWarpMessage input")
	errInvalidIndexInput = errors.New("invalid index to specify warp message")
)

// Signatures of the Warp contract.
var (
	// WarpRawABI contains the raw ABI of Warp contract.
	//go:embed IWarpMessenger.abi
	WarpRawABI string

	WarpABI = contract.ParseABI(WarpRawABI)
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
func (g GasConfig) getBlockchainID(env contract.Environment, _ []byte) ([]byte, error) {
	if err := contract.DeductGas(env, g.GetBlockchainID); err != nil {
		return nil, err
	}
	return PackGetBlockchainIDOutput(common.Hash(env.SnowContext().ChainID))
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

func (g GasConfig) getVerifiedWarpBlockHash(env contract.Environment, input []byte) ([]byte, error) {
	return g.handleWarpMessage(env, input, blockHashHandler{})
}

// UnpackGetVerifiedWarpMessageInput attempts to unpack [input] into the uint32 type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpMessageInput(input []byte) (uint32, error) {
	// We don't use strict mode here because it was disabled with Durango.
	// Since Warp will be deployed after Durango, we don't need to validate padding length.
	var unpacked uint32
	if err := WarpABI.UnpackInputIntoInterface(&unpacked, "getVerifiedWarpMessage", input); err != nil {
		return 0, err
	}
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

// getVerifiedWarpMessage retrieves the pre-verified warp message from the access list and returns
// the expected ABI encoding of the message to the caller.
func (g GasConfig) getVerifiedWarpMessage(env contract.Environment, input []byte) ([]byte, error) {
	return g.handleWarpMessage(env, input, addressedPayloadHandler{})
}

// UnpackSendWarpMessageInput attempts to unpack [input] as []byte
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSendWarpMessageInput(input []byte) ([]byte, error) {
	// We don't use strict mode here because it was disabled with Durango.
	// Since Warp will be deployed after Durango, we don't need to validate padding length.
	var unpacked []byte
	if err := WarpABI.UnpackInputIntoInterface(&unpacked, "sendWarpMessage", input); err != nil {
		return []byte{}, err
	}
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

// sendWarpMessage constructs an Avalanche Warp Message containing an AddressedPayload and emits a log to signal validators that they should
// be willing to sign this message.
func (g GasConfig) sendWarpMessage(env contract.Environment, input []byte) ([]byte, error) {
	if err := contract.DeductGas(env, g.SendWarpMessageBase); err != nil {
		return nil, err
	}
	// This gas cost includes buffer room because it is based off of the total size of the input instead of the produced payload.
	// This ensures that we charge gas before we unpack the variable sized input.
	payloadGas, overflow := math.SafeMul(g.PerWarpMessageByte, uint64(len(input)))
	if overflow {
		payloadGas = math.MaxUint64
	}
	if err := contract.DeductGas(env, payloadGas); err != nil {
		return nil, err
	}
	if env.ReadOnly() {
		return nil, vm.ErrWriteProtection
	}
	// unpack the arguments
	payloadData, err := UnpackSendWarpMessageInput(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSendInput, err)
	}

	var (
		sourceChainID = env.SnowContext().ChainID
		sourceAddress = env.Caller()
	)

	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	if err != nil {
		return nil, err
	}
	unsignedWarpMessage, err := warp.NewUnsignedMessage(
		env.SnowContext().NetworkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	if err != nil {
		return nil, err
	}

	// Add a log to be handled if this action is finalized.
	topics, data, err := PackSendWarpMessageEvent(
		sourceAddress,
		common.Hash(unsignedWarpMessage.ID()),
		unsignedWarpMessage.Bytes(),
	)
	if err != nil {
		return nil, err
	}
	env.StateDB().AddLog(&types.Log{
		Address:     message.ContractAddress,
		Topics:      topics,
		Data:        data,
		BlockNumber: env.BlockNumber().Uint64(),
	})

	return PackSendWarpMessageOutput(common.Hash(unsignedWarpMessage.ID()))
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

// NewPrecompile returns the Warp contract, which charges for its operations
// according to gas.
func NewPrecompile(gas GasConfig) contract.StatefulPrecompiledContract {
	abiFunctionMap := map[string]contract.RunStatefulPrecompileFunc{
		"getBlockchainID":          gas.getBlockchainID,
		"getVerifiedWarpBlockHash": gas.getVerifiedWarpBlockHash,
		"getVerifiedWarpMessage":   gas.getVerifiedWarpMessage,
		"sendWarpMessage":          gas.sendWarpMessage,
	}
	functions := make([]*contract.StatefulPrecompileFunction, 0, len(abiFunctionMap))

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
