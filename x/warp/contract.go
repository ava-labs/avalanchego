// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/accounts/abi"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	predicateutils "github.com/ava-labs/subnet-evm/utils/predicate"
	"github.com/ava-labs/subnet-evm/vmerrs"
	warpPayload "github.com/ava-labs/subnet-evm/warp/payload"

	_ "embed"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
)

const (
	GetVerifiedWarpMessageBaseCost uint64 = 2      // Base cost of entering getVerifiedWarpMessage
	GetBlockchainIDGasCost         uint64 = 2      // Based on GasQuickStep used in existing EVM instructions
	AddWarpMessageGasCost          uint64 = 20_000 // Cost of producing and serving a BLS Signature
	// Sum of base log gas cost, cost of producing 4 topics, and producing + serving a BLS Signature (sign + trie write)
	// Note: using trie write for the gas cost results in a conservative overestimate since the message is stored in a
	// flat database that can be cleaned up after a period of time instead of the EVM trie.

	SendWarpMessageGasCost uint64 = params.LogGas + 4*params.LogTopicGas + AddWarpMessageGasCost + contract.WriteGasCostPerSlot
	// SendWarpMessageGasCostPerByte cost accounts for producing a signed message of a given size
	SendWarpMessageGasCostPerByte uint64 = params.LogDataGas

	GasCostPerWarpSigner            uint64 = 500
	GasCostPerWarpMessageBytes      uint64 = 100
	GasCostPerSignatureVerification uint64 = 200_000
)

var (
	errInvalidSendInput = errors.New("invalid sendWarpMessage input")
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
	DestinationChainID  common.Hash
	DestinationAddress  common.Address
	Payload             []byte
}

type GetVerifiedWarpMessageOutput struct {
	Message WarpMessage
	Valid   bool
}
type SendWarpMessageInput struct {
	DestinationChainID common.Hash
	DestinationAddress common.Address
	Payload            []byte
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
	res, err := WarpABI.UnpackInput("getVerifiedWarpBlockHash", input)
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
	remainingGas, err = contract.DeductGas(suppliedGas, GetVerifiedWarpMessageBaseCost)
	if err != nil {
		return nil, remainingGas, err
	}
	predicateBytes, valid := unpackWarpMessage(accessibleState, input)
	// If there is no such value or it failed verification, return invalid
	if !valid {
		packedOutput, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
			Valid: false,
		})
		if err != nil {
			return nil, remainingGas, err
		}
		return packedOutput, remainingGas, nil
	}

	// Note: we charge for the size of the message during both predicate verification and each time the message is read during
	// EVM execution because each execution incurs an additional read cost.
	msgBytesGas, overflow := math.SafeMul(GasCostPerWarpMessageBytes, uint64(len(predicateBytes)))
	if overflow {
		return nil, remainingGas, vmerrs.ErrOutOfGas
	}
	if remainingGas, err = contract.DeductGas(remainingGas, msgBytesGas); err != nil {
		return nil, 0, err
	}
	// Note: since the predicate is verified in advance of execution, the precompile should not
	// hit an error during execution.
	unpackedPredicateBytes, err := predicateutils.UnpackPredicate(predicateBytes)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidPredicateBytes, err)
	}
	warpMessage, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidWarpMsg, err)
	}

	blockHashPayload, err := warpPayload.ParseBlockHashPayload(warpMessage.UnsignedMessage.Payload)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidBlockHashPayload, err)
	}
	packedOutput, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
		WarpBlockHash: WarpBlockHash{
			SourceChainID: common.Hash(warpMessage.SourceChainID),
			BlockHash:     blockHashPayload.BlockHash,
		},
		Valid: true,
	})
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// UnpackGetVerifiedWarpMessageInput attempts to unpack [input] into the uint32 type argument
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackGetVerifiedWarpMessageInput(input []byte) (uint32, error) {
	res, err := WarpABI.UnpackInput("getVerifiedWarpMessage", input)
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

func unpackWarpMessage(accessibleState contract.AccessibleState, input []byte) ([]byte, bool) {
	warpIndex, err := UnpackGetVerifiedWarpMessageInput(input)
	if err != nil {
		return nil, false
	}
	state := accessibleState.GetStateDB()
	predicateBytes, exists := state.GetPredicateStorageSlots(ContractAddress, warpIndex)
	predicateResults := accessibleState.GetBlockContext().GetPredicateResults(state.GetTxHash(), ContractAddress)
	valid := set.BitsFromBytes(predicateResults).Contains(int(warpIndex))
	return predicateBytes, exists && valid
}

// getVerifiedWarpMessage retrieves the pre-verified warp message from the predicate storage slots and returns
// the expected ABI encoding of the message to the caller.
func getVerifiedWarpMessage(accessibleState contract.AccessibleState, caller common.Address, addr common.Address, input []byte, suppliedGas uint64, readOnly bool) (ret []byte, remainingGas uint64, err error) {
	remainingGas, err = contract.DeductGas(suppliedGas, GetVerifiedWarpMessageBaseCost)
	if err != nil {
		return nil, remainingGas, err
	}
	predicateBytes, valid := unpackWarpMessage(accessibleState, input)
	// If there is no such value or it failed verification, return invalid
	if !valid {
		packedOutput, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
			Valid: false,
		})
		if err != nil {
			return nil, remainingGas, err
		}
		return packedOutput, remainingGas, nil
	}

	// Note: we charge for the size of the message during both predicate verification and each time the message is read during
	// EVM execution because each execution incurs an additional read cost.
	msgBytesGas, overflow := math.SafeMul(GasCostPerWarpMessageBytes, uint64(len(predicateBytes)))
	if overflow {
		return nil, remainingGas, vmerrs.ErrOutOfGas
	}
	if remainingGas, err = contract.DeductGas(remainingGas, msgBytesGas); err != nil {
		return nil, 0, err
	}
	// Note: since the predicate is verified in advance of execution, the precompile should not
	// hit an error during execution.
	unpackedPredicateBytes, err := predicateutils.UnpackPredicate(predicateBytes)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidPredicateBytes, err)
	}
	warpMessage, err := warp.ParseMessage(unpackedPredicateBytes)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidWarpMsg, err)
	}

	addressedPayload, err := warpPayload.ParseAddressedPayload(warpMessage.UnsignedMessage.Payload)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidAddressedPayload, err)
	}
	packedOutput, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
		Message: WarpMessage{
			SourceChainID:       common.Hash(warpMessage.SourceChainID),
			OriginSenderAddress: addressedPayload.SourceAddress,
			DestinationChainID:  addressedPayload.DestinationChainID,
			DestinationAddress:  addressedPayload.DestinationAddress,
			Payload:             addressedPayload.Payload,
		},
		Valid: true,
	})
	if err != nil {
		return nil, remainingGas, err
	}

	// Return the packed output and the remaining gas
	return packedOutput, remainingGas, nil
}

// UnpackSendWarpMessageInput attempts to unpack [input] as SendWarpMessageInput
// assumes that [input] does not include selector (omits first 4 func signature bytes)
func UnpackSendWarpMessageInput(input []byte) (SendWarpMessageInput, error) {
	inputStruct := SendWarpMessageInput{}
	err := WarpABI.UnpackInputIntoInterface(&inputStruct, "sendWarpMessage", input)

	return inputStruct, err
}

// PackSendWarpMessage packs [inputStruct] of type SendWarpMessageInput into the appropriate arguments for sendWarpMessage.
func PackSendWarpMessage(inputStruct SendWarpMessageInput) ([]byte, error) {
	return WarpABI.Pack("sendWarpMessage", inputStruct.DestinationChainID, inputStruct.DestinationAddress, inputStruct.Payload)
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
		return nil, 0, vmerrs.ErrOutOfGas
	}
	if remainingGas, err = contract.DeductGas(remainingGas, payloadGas); err != nil {
		return nil, 0, err
	}
	if readOnly {
		return nil, remainingGas, vmerrs.ErrWriteProtection
	}
	// unpack the arguments
	inputStruct, err := UnpackSendWarpMessageInput(input)
	if err != nil {
		return nil, remainingGas, fmt.Errorf("%w: %s", errInvalidSendInput, err)
	}

	var (
		sourceChainID      = accessibleState.GetSnowContext().ChainID
		destinationChainID = inputStruct.DestinationChainID
		sourceAddress      = caller
		destinationAddress = inputStruct.DestinationAddress
		payload            = inputStruct.Payload
	)

	addressedPayload, err := warpPayload.NewAddressedPayload(
		sourceAddress,
		destinationChainID,
		destinationAddress,
		payload,
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
	accessibleState.GetStateDB().AddLog(
		ContractAddress,
		[]common.Hash{
			WarpABI.Events["SendWarpMessage"].ID,
			destinationChainID,
			destinationAddress.Hash(),
			sourceAddress.Hash(),
		},
		unsignedWarpMessage.Bytes(),
		accessibleState.GetBlockContext().Number().Uint64(),
	)

	// Return an empty output and the remaining gas
	return []byte{}, remainingGas, nil
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
