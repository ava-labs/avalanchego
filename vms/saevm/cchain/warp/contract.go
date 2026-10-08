// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/ava-labs/libevm/libevm"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/evm/precompile"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

const selectorLen = 4

var (
	errMissingSelector         = errors.New("missing function selector")
	errUnknownSelector         = errors.New("unknown function selector")
	errInvalidSendInput        = errors.New("invalid sendWarpMessage input")
	errInvalidIndexInput       = errors.New("invalid index to specify warp message")
	errNoPredicateReader       = errors.New("EVM state does not expose access-list predicates")
	errInvalidPredicateResults = errors.New("cannot parse predicate results from block header")
	errInvalidAddressedPayload = errors.New("cannot unpack addressed payload")
	errInvalidBlockHashPayload = errors.New("cannot unpack block hash payload")
)

// Function selectors, resolved once from the ABI.
var (
	selGetBlockchainID          = selector("getBlockchainID")
	selGetVerifiedWarpMessage   = selector("getVerifiedWarpMessage")
	selGetVerifiedWarpBlockHash = selector("getVerifiedWarpBlockHash")
	selSendWarpMessage          = selector("sendWarpMessage")

	// Prepacked `Valid: false` outputs.
	invalidMessageOutput   = mustPack(PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{}))
	invalidBlockHashOutput = mustPack(PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{}))
)

func selector(method string) string {
	m, ok := ABI.Methods[method]
	if !ok {
		panic(fmt.Sprintf("method %q missing from warp ABI", method))
	}
	return string(m.ID)
}

func mustPack(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

// NewPrecompile returns the warp precompile for the chain described by ctx.
// The returned contract MUST only be invoked by the EVM, as required by
// [vm.NewStatefulPrecompile].
func NewPrecompile(ctx *snow.Context) libevm.PrecompiledContract {
	c := &contract{ctx: ctx}
	return vm.NewStatefulPrecompile(c.run)
}

type contract struct {
	ctx *snow.Context
}

// run dispatches on the 4-byte selector. It mirrors coreth's precompile
// adapter and contract, under Granite rules only.
func (c *contract) run(env vm.PrecompileEnvironment, input []byte) ([]byte, error) {
	switch env.IncomingCallType() {
	case vm.DelegateCall, vm.CallCode:
		// Rejected since Granite; all gas is consumed.
		env.UseGas(env.Gas())
		return nil, vm.ErrExecutionReverted
	}

	if len(input) < selectorLen {
		return nil, fmt.Errorf("%w: input length %d", errMissingSelector, len(input))
	}
	sel, args := string(input[:selectorLen]), input[selectorLen:]
	switch sel {
	case selGetBlockchainID:
		return c.getBlockchainID(env)
	case selGetVerifiedWarpMessage:
		return c.getVerified(env, args, unpackGetVerifiedWarpMessageInput, invalidMessageOutput, handleAddressedCall)
	case selGetVerifiedWarpBlockHash:
		return c.getVerified(env, args, unpackGetVerifiedWarpBlockHashInput, invalidBlockHashOutput, handleBlockHash)
	case selSendWarpMessage:
		return c.sendWarpMessage(env, args)
	default:
		return nil, fmt.Errorf("%w: %#x", errUnknownSelector, sel)
	}
}

// useGas charges gas, returning [vm.ErrOutOfGas] if it is insufficient.
func useGas(env vm.PrecompileEnvironment, gas uint64) error {
	if !env.UseGas(gas) {
		return vm.ErrOutOfGas
	}
	return nil
}

func (c *contract) getBlockchainID(env vm.PrecompileEnvironment) ([]byte, error) {
	if err := useGas(env, Gas.GetBlockchainID); err != nil {
		return nil, err
	}
	return PackGetBlockchainIDOutput(common.Hash(c.ctx.ChainID))
}

// getVerified serves a pre-verified warp message at the index given by args,
// or `invalid` if the predicate is missing or failed verification.
func (*contract) getVerified(
	env vm.PrecompileEnvironment,
	args []byte,
	unpackIndex func([]byte) (uint32, error),
	invalid []byte,
	handle func(*avalanchewarp.Message) ([]byte, error),
) ([]byte, error) {
	if err := useGas(env, Gas.GetVerifiedWarpMessageBase); err != nil {
		return nil, err
	}
	index32, err := unpackIndex(args)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidIndexInput, err)
	}
	if index32 > math.MaxInt32 {
		return nil, fmt.Errorf("%w: %d larger than MaxInt32", errInvalidIndexInput, index32)
	}
	index := int(index32) // bounded above by MaxInt32

	reader, ok := env.ReadOnlyState().(precompile.PredicateReader)
	if !ok {
		return nil, invalidate(env, errNoPredicateReader)
	}
	pred, exists := reader.GetPredicate(ContractAddress, index)

	results, err := blockResults(env)
	if err != nil {
		return nil, err
	}
	failed := results.Get(env.ReadOnlyState().TxHash(), ContractAddress)
	if !exists || failed.Contains(index) {
		return invalid, nil
	}

	// The message is charged for again on every read, since each read
	// incurs the serialization cost.
	readGas, err := safemath.Mul(Gas.PerWarpMessageChunk, uint64(len(pred)))
	if err != nil {
		return nil, vm.ErrOutOfGas
	}
	if err := useGas(env, readGas); err != nil {
		return nil, err
	}

	// Verified in advance of execution, so these should never fail.
	b, err := pred.Bytes()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidPredicateBytes, err)
	}
	msg, err := avalanchewarp.ParseMessage(b)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidWarpMsg, err)
	}
	return handle(msg)
}

// blockResults reads the predicate results recorded in the block header.
// Under Helicon rules the header's extra data is exactly the encoded results.
//
// Block verification guarantees that the extra data parses, so a parse
// failure is a wiring bug. It fails closed by invalidating the execution
// (rather than merely failing the transaction), which surfaces as an error
// from libevm's core.ApplyTransaction and is therefore fatal to SAE. This is
// intentional and mirrors coreth, whose adapter panics in the same case.
func blockResults(env vm.PrecompileEnvironment) (predicate.BlockResults, error) {
	header, err := env.BlockHeader()
	if err != nil {
		return nil, err
	}
	if len(header.Extra) == 0 {
		return predicate.BlockResults{}, nil
	}
	results, err := predicate.ParseBlockResults(header.Extra)
	if err != nil {
		return nil, invalidate(env, fmt.Errorf("%w: %w", errInvalidPredicateResults, err))
	}
	return results, nil
}

// invalidate voids the execution of the calling transaction with err, and
// returns err. Unlike a plain precompile error, which only fails the
// transaction, an invalidated execution is returned as an error by
// libevm's core.ApplyTransaction. It is reserved for wiring bugs that are
// unreachable for validated blocks.
func invalidate(env vm.PrecompileEnvironment, err error) error {
	env.InvalidateExecution(err)
	return err
}

func handleAddressedCall(msg *avalanchewarp.Message) ([]byte, error) {
	addressed, err := payload.ParseAddressedCall(msg.UnsignedMessage.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidAddressedPayload, err)
	}
	return PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
		Message: WarpMessage{
			SourceChainID:       common.Hash(msg.SourceChainID),
			OriginSenderAddress: common.BytesToAddress(addressed.SourceAddress),
			Payload:             addressed.Payload,
		},
		Valid: true,
	})
}

func handleBlockHash(msg *avalanchewarp.Message) ([]byte, error) {
	hash, err := payload.ParseHash(msg.UnsignedMessage.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidBlockHashPayload, err)
	}
	return PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
		WarpBlockHash: WarpBlockHash{
			SourceChainID: common.Hash(msg.SourceChainID),
			BlockHash:     common.BytesToHash(hash.Hash[:]),
		},
		Valid: true,
	})
}

// sendWarpMessage emits an unsigned warp message, with the caller as its
// source address, as a log for validators to sign once the block executes.
func (c *contract) sendWarpMessage(env vm.PrecompileEnvironment, args []byte) ([]byte, error) {
	if err := useGas(env, Gas.SendWarpMessageBase); err != nil {
		return nil, err
	}
	// Charged on the raw input size, before unpacking, so the variable-size
	// payload is paid for before it is parsed.
	payloadGas, err := safemath.Mul(Gas.PerWarpMessageByte, uint64(len(args)))
	if err != nil {
		return nil, vm.ErrOutOfGas
	}
	if err := useGas(env, payloadGas); err != nil {
		return nil, err
	}
	if env.ReadOnly() {
		return nil, vm.ErrWriteProtection
	}

	payloadData, err := unpackSendWarpMessageInput(args)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errInvalidSendInput, err)
	}

	sourceAddress := env.Addresses().EVMSemantic.Caller
	addressed, err := payload.NewAddressedCall(sourceAddress.Bytes(), payloadData)
	if err != nil {
		return nil, err
	}
	unsigned, err := avalanchewarp.NewUnsignedMessage(c.ctx.NetworkID, c.ctx.ChainID, addressed.Bytes())
	if err != nil {
		return nil, err
	}

	topics, data, err := PackSendWarpMessageEvent(sourceAddress, common.Hash(unsigned.ID()), unsigned.Bytes())
	if err != nil {
		return nil, err
	}
	env.StateDB().AddLog(&types.Log{
		Address:     ContractAddress,
		Topics:      topics,
		Data:        data,
		BlockNumber: env.BlockNumber().Uint64(),
	})
	return PackSendWarpMessageOutput(common.Hash(unsigned.ID()))
}
