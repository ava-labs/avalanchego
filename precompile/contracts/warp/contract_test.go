// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/subnet-evm/core/extstate"
	"github.com/ava-labs/subnet-evm/params/extras"
	"github.com/ava-labs/subnet-evm/precompile/contract"
	"github.com/ava-labs/subnet-evm/precompile/precompiletest"
	"github.com/ava-labs/subnet-evm/utils/utilstest"

	agoUtils "github.com/ava-labs/avalanchego/utils"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestGetBlockchainID(t *testing.T) {
	callerAddr := common.HexToAddress("0x0123")

	defaultSnowCtx := utilstest.NewTestSnowContext(t)
	blockchainID := defaultSnowCtx.ChainID

	tests := map[string]precompiletest.PrecompileTest{
		"getBlockchainID success": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetBlockchainIDGasCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				expectedOutput, err := PackGetBlockchainIDOutput(common.Hash(blockchainID))
				require.NoError(t, err)

				return expectedOutput
			}(),
		},
		"getBlockchainID readOnly": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetBlockchainIDGasCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				expectedOutput, err := PackGetBlockchainIDOutput(common.Hash(blockchainID))
				require.NoError(t, err)

				return expectedOutput
			}(),
		},
		"getBlockchainID insufficient gas": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetBlockchainID()
				require.NoError(t, err)

				return input
			},
			SuppliedGas: GetBlockchainIDGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
	}

	precompiletest.RunPrecompileTests(t, Module, tests)
}

func TestSendWarpMessage(t *testing.T) {
	callerAddr := common.HexToAddress("0x0123")

	defaultSnowCtx := utilstest.NewTestSnowContext(t)
	blockchainID := defaultSnowCtx.ChainID
	sendWarpMessagePayload := agoUtils.RandomBytes(100)

	sendWarpMessageInput, err := PackSendWarpMessage(sendWarpMessagePayload)
	require.NoError(t, err)
	sendWarpMessageAddressedPayload, err := payload.NewAddressedCall(
		callerAddr.Bytes(),
		sendWarpMessagePayload,
	)
	require.NoError(t, err)
	unsignedWarpMessage, err := avalancheWarp.NewUnsignedMessage(
		defaultSnowCtx.NetworkID,
		blockchainID,
		sendWarpMessageAddressedPayload.Bytes(),
	)
	require.NoError(t, err)

	tests := map[string]precompiletest.PrecompileTest{
		"send warp message readOnly": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)),
			ReadOnly:    true,
			ExpectedErr: vm.ErrWriteProtection.Error(),
		},
		"send warp message insufficient gas for first step": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"send warp message insufficient gas for payload bytes": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"send warp message invalid input": {
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return sendWarpMessageInput[:4] // Include only the function selector, so that the input is invalid
			},
			SuppliedGas: SendWarpMessageGasCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidSendInput.Error(),
		},
		"send warp message success": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return sendWarpMessageInput },
			SuppliedGas: SendWarpMessageGasCost + uint64(len(sendWarpMessageInput[4:])*int(SendWarpMessageGasCostPerByte)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				bytes, err := PackSendWarpMessageOutput(common.Hash(unsignedWarpMessage.ID()))
				if err != nil {
					panic(err)
				}
				return bytes
			}(),
			AfterHook: func(t testing.TB, state *extstate.StateDB) {
				logs := state.Logs()
				require.Len(t, logs, 1)
				log := logs[0]
				require.Equal(
					t,
					[]common.Hash{
						WarpABI.Events["SendWarpMessage"].ID,
						common.BytesToHash(callerAddr[:]),
						common.Hash(unsignedWarpMessage.ID()),
					},
					log.Topics,
					WarpABI.Events["SendWarpMessage"].ID,
				)

				unsignedWarpMsg, err := UnpackSendWarpEventDataToMessage(log.Data)
				require.NoError(t, err)
				addressedPayload, err := payload.ParseAddressedCall(unsignedWarpMsg.Payload)
				require.NoError(t, err)

				require.Equal(t, common.BytesToAddress(addressedPayload.SourceAddress), callerAddr)
				require.Equal(t, unsignedWarpMsg.SourceChainID, blockchainID)
				require.Equal(t, addressedPayload.Payload, sendWarpMessagePayload)
			},
		},
	}

	precompiletest.RunPrecompileTests(t, Module, tests)
}

func TestGetVerifiedWarpMessage(t *testing.T) {
	networkID := uint32(54321)
	preGraniteGasConfig := CurrentGasConfig(extras.AvalancheRules{IsGranite: false})
	postGraniteGasConfig := CurrentGasConfig(extras.AvalancheRules{IsGranite: true})
	callerAddr := common.HexToAddress("0x0123")
	sourceAddress := common.HexToAddress("0x456789")
	sourceChainID := ids.GenerateTestID()
	packagedPayloadBytes := []byte("mcsorley")
	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		packagedPayloadBytes,
	)
	require.NoError(t, err)
	unsignedWarpMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, addressedPayload.Bytes())
	require.NoError(t, err)
	warpMessage, err := avalancheWarp.NewMessage(unsignedWarpMsg, &avalancheWarp.BitSetSignature{}) // Create message with empty signature for testing
	require.NoError(t, err)
	warpMessagePredicate := predicate.New(warpMessage.Bytes())
	getVerifiedWarpMsg, err := PackGetVerifiedWarpMessage(0)
	require.NoError(t, err)

	// Invalid warp message predicate
	invalidWarpMsgPredicate := predicate.New([]byte{1, 2, 3})

	// Invalid addressed payload predicate and chunk length
	invalidAddrUnsigned, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	require.NoError(t, err)
	invalidAddrWarpMsg, err := avalancheWarp.NewMessage(invalidAddrUnsigned, &avalancheWarp.BitSetSignature{})
	require.NoError(t, err)
	invalidAddressedPredicate := predicate.New(invalidAddrWarpMsg.Bytes())

	// Invalid predicate packing by corrupting a valid predicate
	invalidPackedPredicate := predicate.Predicate{{}}

	noFailures := set.NewBits()
	require.Empty(t, noFailures.Bytes())

	tests := map[string]precompiletest.PrecompileTest{
		"get message success": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of bounds non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0))
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message failure non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0, 1))
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get non-existent message": {
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpMsg },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success readOnly": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get non-existent message readOnly": {
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpMsg },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of gas for base cost": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates:  []predicate.Predicate{warpMessagePredicate},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message out of gas": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message invalid predicate packing": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		"get message invalid warp message": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		"get message invalid addressed payload": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidAddressedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidAddressedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidAddressedPayload.Error(),
		},
		"get message index invalid uint32": {
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return append(WarpABI.Methods["getVerifiedWarpMessage"].ID, new(big.Int).SetInt64(math.MaxInt64).Bytes()...)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message index invalid int32": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				res, err := PackGetVerifiedWarpMessage(math.MaxInt32 + 1)
				require.NoError(t, err)
				return res
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message invalid index input bytes": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				res, err := PackGetVerifiedWarpMessage(1)
				require.NoError(t, err)
				return res[:len(res)-2]
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message success granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success non-zero index granite": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpMessage(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0))
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success readOnly granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpMessageOutput(GetVerifiedWarpMessageOutput{
					Message: WarpMessage{
						SourceChainID:       common.Hash(sourceChainID),
						OriginSenderAddress: sourceAddress,
						Payload:             packagedPayloadBytes,
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of gas granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message invalid predicate packing granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		"get message invalid warp message granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		"get message invalid addressed payload granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpMsg },
			Predicates: []predicate.Predicate{invalidAddressedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidAddressedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidAddressedPayload.Error(),
		},
	}

	precompiletest.RunPrecompileTests(t, Module, tests)
}

func TestGetVerifiedWarpBlockHash(t *testing.T) {
	networkID := uint32(54321)
	preGraniteGasConfig := CurrentGasConfig(extras.AvalancheRules{IsGranite: false})
	postGraniteGasConfig := CurrentGasConfig(extras.AvalancheRules{IsGranite: false})
	callerAddr := common.HexToAddress("0x0123")
	sourceChainID := ids.GenerateTestID()
	blockHash := ids.GenerateTestID()
	blockHashPayload, err := payload.NewHash(blockHash)
	require.NoError(t, err)
	unsignedWarpMsg, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, blockHashPayload.Bytes())
	require.NoError(t, err)
	warpMessage, err := avalancheWarp.NewMessage(unsignedWarpMsg, &avalancheWarp.BitSetSignature{}) // Create message with empty signature for testing
	require.NoError(t, err)
	warpMessagePredicate := predicate.New(warpMessage.Bytes())
	getVerifiedWarpBlockHash, err := PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err)

	// Invalid warp message predicate
	invalidWarpMsgPredicate := predicate.New([]byte{1, 2, 3})

	// Invalid block hash payload predicate
	invalidHashUnsigned, err := avalancheWarp.NewUnsignedMessage(networkID, sourceChainID, []byte{1, 2, 3})
	require.NoError(t, err)
	invalidHashWarpMsg, err := avalancheWarp.NewMessage(invalidHashUnsigned, &avalancheWarp.BitSetSignature{})
	require.NoError(t, err)
	invalidHashPredicate := predicate.New(invalidHashWarpMsg.Bytes())

	// Invalid predicate packing by corrupting a valid predicate
	invalidPackedPredicate := predicate.Predicate{{}}

	noFailures := set.NewBits()
	require.Empty(t, noFailures.Bytes())

	tests := map[string]precompiletest.PrecompileTest{
		"get message success": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of bounds non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0))
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message failure non-zero index": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0, 1))
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get non-existent message": {
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success readOnly": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get non-existent message readOnly": {
			Caller:  callerAddr,
			InputFn: func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{Valid: false})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of gas for base cost": {
			Caller:      callerAddr,
			InputFn:     func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Rules:       extras.AvalancheRules{IsGranite: false},
			Predicates:  []predicate.Predicate{warpMessagePredicate},
			SuppliedGas: GetVerifiedWarpMessageBaseCost - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message out of gas": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message invalid predicate packing": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		"get message invalid warp message": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		"get message invalid block hash payload": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidHashPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + preGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidHashPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidBlockHashPayload.Error(),
		},
		"get message index invalid uint32": {
			Caller: callerAddr,
			InputFn: func(testing.TB) []byte {
				return append(WarpABI.Methods["getVerifiedWarpBlockHash"].ID, new(big.Int).SetInt64(math.MaxInt64).Bytes()...)
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message index invalid int32": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				res, err := PackGetVerifiedWarpBlockHash(math.MaxInt32 + 1)
				require.NoError(t, err)
				return res
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message invalid index input bytes": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				res, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(t, err)
				return res[:len(res)-2]
			},
			Rules:       extras.AvalancheRules{IsGranite: false},
			SuppliedGas: GetVerifiedWarpMessageBaseCost,
			ReadOnly:    false,
			ExpectedErr: errInvalidIndexInput.Error(),
		},
		"get message success granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success non-zero index granite": {
			Caller: callerAddr,
			InputFn: func(t testing.TB) []byte {
				input, err := PackGetVerifiedWarpBlockHash(1)
				require.NoError(t, err)
				return input
			},
			Predicates: []predicate.Predicate{{}, warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(set.NewBits(0))
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    false,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message success readOnly granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)),
			ReadOnly:    true,
			ExpectedRes: func() []byte {
				res, err := PackGetVerifiedWarpBlockHashOutput(GetVerifiedWarpBlockHashOutput{
					WarpBlockHash: WarpBlockHash{
						SourceChainID: common.Hash(sourceChainID),
						BlockHash:     common.Hash(blockHash),
					},
					Valid: true,
				})
				if err != nil {
					panic(err)
				}
				return res
			}(),
		},
		"get message out of gas granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{warpMessagePredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(warpMessagePredicate)) - 1,
			ReadOnly:    false,
			ExpectedErr: vm.ErrOutOfGas.Error(),
		},
		"get message invalid predicate packing granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidPackedPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidPackedPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidPredicateBytes.Error(),
		},
		"get message invalid warp message granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidWarpMsgPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidWarpMsgPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidWarpMsg.Error(),
		},
		"get message invalid block hash payload granite": {
			Caller:     callerAddr,
			InputFn:    func(testing.TB) []byte { return getVerifiedWarpBlockHash },
			Predicates: []predicate.Predicate{invalidHashPredicate},
			SetupBlockContext: func(mbc *contract.MockBlockContext) {
				mbc.EXPECT().GetPredicateResults(common.Hash{}, ContractAddress).Return(noFailures)
			},
			Rules:       extras.AvalancheRules{IsGranite: true},
			SuppliedGas: GetVerifiedWarpMessageBaseCost + postGraniteGasConfig.PerWarpMessageChunk*uint64(len(invalidHashPredicate)),
			ReadOnly:    false,
			ExpectedErr: errInvalidBlockHashPayload.Error(),
		},
	}

	precompiletest.RunPrecompileTests(t, Module, tests)
}

func TestPackEvents(t *testing.T) {
	sourceChainID := ids.GenerateTestID()
	sourceAddress := common.HexToAddress("0x0123")
	payloadData := []byte("mcsorley")
	networkID := uint32(54321)

	addressedPayload, err := payload.NewAddressedCall(
		sourceAddress.Bytes(),
		payloadData,
	)
	require.NoError(t, err)

	unsignedWarpMessage, err := avalancheWarp.NewUnsignedMessage(
		networkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(t, err)

	topics, data, err := PackSendWarpMessageEvent(
		sourceAddress,
		common.Hash(unsignedWarpMessage.ID()),
		unsignedWarpMessage.Bytes(),
	)
	require.NoError(t, err)
	require.Equal(
		t,
		[]common.Hash{
			WarpABI.Events["SendWarpMessage"].ID,
			common.BytesToHash(sourceAddress[:]),
			common.Hash(unsignedWarpMessage.ID()),
		},
		topics,
	)

	unpacked, err := UnpackSendWarpEventDataToMessage(data)
	require.NoError(t, err)
	require.Equal(t, unsignedWarpMessage.Bytes(), unpacked.Bytes())
}
