// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/evm/predicate"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/blocks"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/warp"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// withWarpEnabled enables the Warp precompile at genesis, so the block
// builder populates predicate results in `header.Extra` and calls to
// `warpcontract.ContractAddress` reach the precompile.
func withWarpEnabled() sutOption {
	return withGenesisPrecompiles(extras.Precompiles{
		warpcontract.ConfigKey: warpcontract.NewDefaultConfig(new(testGenesisTime)),
	})
}

// TestSendWarpMessage checks availability of warp verification requests
// relative to block execution.
func TestSendWarpMessage(t *testing.T) {
	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...), withWarpEnabled())

	payloadData := utils.RandomBytes(100)

	warpSendMessageInput, err := warpcontract.PackSendWarpMessage(payloadData)
	require.NoError(t, err)
	sut.send(ctx, t, wallet, 0, warpTx(warpSendMessageInput, nil /*unsigned message*/))

	built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))
	require.Len(t, built.EthBlock().Transactions(), 1)

	// The validator will not sign any messages, since the transaction is not executed yet.

	addressedPayload, err := payload.NewAddressedCall(wallet.Addresses()[0].Bytes(), payloadData)
	require.NoError(t, err)
	unsignedMessage := sut.newUnsignedWarpMessage(t, addressedPayload.Bytes())
	sut.verifyWarpMessage(ctx, t, unsignedMessage.Bytes(), int32(warp.SourceAddressErrCode))

	blockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(t, err)
	blockMessage := sut.newUnsignedWarpMessage(t, blockHashPayload.Bytes())
	sut.verifyWarpMessage(ctx, t, blockMessage.Bytes(), int32(saewarp.NotAcceptedErrCode))

	sut.AcceptAndExecute(ctx, t, built)

	// Receipts are generated after block execution
	receipts := built.Receipts()
	require.Len(t, receipts, 1)
	require.Len(t, receipts[0].Logs, 1)
	expectedTopics := []common.Hash{
		warpcontract.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(wallet.Addresses()[0].Bytes()),
		common.Hash(unsignedMessage.ID()),
	}
	require.Equal(t, expectedTopics, receipts[0].Logs[0].Topics)

	// Unsigned warp message should have been emitted in the log.
	logData := receipts[0].Logs[0].Data
	loggedMessage, err := warpcontract.UnpackSendWarpEventDataToMessage(logData)
	require.NoError(t, err)
	require.Equal(t, unsignedMessage, loggedMessage)

	// The messages should be verifiable after the block is executed.
	sut.verifyWarpMessage(ctx, t, unsignedMessage.Bytes(), 0 /* valid message */)
	sut.verifyWarpMessage(ctx, t, blockMessage.Bytes(), 0 /* valid message */)
}

// TestInactiveWarpAddressLogsNotSigned checks that, while the Warp precompile
// is inactive, logs emitted from the Warp address MUST NOT become signable
// messages, even when they are well-formed `SendWarpMessage` events.
func TestInactiveWarpAddressLogsNotSigned(t *testing.T) {
	// Claims a source address that never sent it.
	forgedPayload, err := payload.NewAddressedCall(utils.RandomBytes(20), utils.RandomBytes(32))
	require.NoError(t, err)
	chainID := ids.GenerateTestID()
	forged, err := avalancheWarp.NewUnsignedMessage(constants.UnitTestID, chainID, forgedPayload.Bytes())
	require.NoError(t, err)

	wallet := newWallet(t, 1)
	ctx, sut := newSUT(
		t,
		withChainID(chainID),
		withMaxAllocFor(wallet.Addresses()...),
		withAccount(warpcontract.ContractAddress, types.Account{
			Balance: big.NewInt(0),
			Code:    sendWarpMessageLogCode(t, forged),
		}),
	)

	sut.send(ctx, t, wallet, 0, warpTx(nil /*txPayload*/, nil /*signedMessage*/))
	built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))
	sut.AcceptAndExecute(ctx, t, built)

	receipts := built.Receipts()
	require.Len(t, receipts, 1)
	require.Len(t, receipts[0].Logs, 1, "the code at the Warp address must have emitted its log")
	sut.verifyWarpMessage(ctx, t, forged.Bytes(), int32(warp.SourceAddressErrCode))
}

// sendWarpMessageLogCode returns EVM code that emits the `SendWarpMessage` log
// the Warp precompile would emit for msg.
func sendWarpMessageLogCode(t *testing.T, msg *avalancheWarp.UnsignedMessage) []byte {
	t.Helper()

	topics, data, err := warpcontract.PackSendWarpMessageEvent(common.Address{}, common.Hash(msg.ID()), msg.Bytes())
	require.NoError(t, err)
	require.Len(t, topics, 3)

	push2 := func(v int) []byte {
		return []byte{byte(vm.PUSH2), byte(v >> 8), byte(v)}
	}
	// Copies data, appended after the prefix, into memory and LOG3s it. The
	// prefix length doesn't depend on dataOffset.
	prefix := func(dataOffset int) []byte {
		return slices.Concat(
			push2(len(data)), push2(dataOffset), []byte{byte(vm.PUSH1), 0, byte(vm.CODECOPY)},
			[]byte{byte(vm.PUSH32)}, topics[2].Bytes(),
			[]byte{byte(vm.PUSH32)}, topics[1].Bytes(),
			[]byte{byte(vm.PUSH32)}, topics[0].Bytes(),
			push2(len(data)), []byte{byte(vm.PUSH1), 0, byte(vm.LOG3), byte(vm.STOP)},
		)
	}
	return slices.Concat(prefix(len(prefix(0))), data)
}

func TestPredicateVerification(t *testing.T) {
	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...), withWarpEnabled())

	sourceAddress := wallet.Addresses()[0]
	addressedPayload, err := payload.NewAddressedCall(sourceAddress.Bytes(), []byte{1, 2, 3})
	require.NoError(t, err)
	addressedCallMessage := sut.newUnsignedWarpMessage(t, addressedPayload.Bytes())
	addressedCallTxPayload, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err)

	blockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	blockHashMessage := sut.newUnsignedWarpMessage(t, blockHashPayload.Bytes())
	blockHashTxPayload, err := warpcontract.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err)

	tests := []struct {
		name           string
		validPredicate bool
		signedMsg      *avalancheWarp.Message
		txPayload      []byte
	}{
		{
			name:           "valid warp message",
			validPredicate: true,
			signedMsg:      sut.validators.Sign(t, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "invalid warp message",
			validPredicate: false,
			signedMsg:      warptest.IncorrectlySign(t, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "valid warp block hash",
			validPredicate: true,
			signedMsg:      sut.validators.Sign(t, blockHashMessage),
			txPayload:      blockHashTxPayload,
		},
		{
			name:           "invalid warp block hash",
			validPredicate: false,
			signedMsg:      warptest.IncorrectlySign(t, blockHashMessage),
			txPayload:      blockHashTxPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sut.clock.Advance(blockBuildAdvance)
			validateTx := sut.send(ctx, t, wallet, 0, warpTx(tt.txPayload, tt.signedMsg))

			built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t), vmtest.WithBlockContext(&block.Context{PChainHeight: 0}))
			require.Len(t, built.EthBlock().Transactions(), 1)

			sut.AcceptAndExecute(ctx, t, built)
			assertPredicateResult(t, built, validateTx, tt.validPredicate)

			receipts := built.Receipts()
			require.Len(t, receipts, 1)
			require.Equal(t, types.ReceiptStatusSuccessful, receipts[0].Status)
		})
	}
}

func assertPredicateResult(
	t *testing.T,
	built *blocks.Block,
	validateTx *types.Transaction,
	expectValid bool,
) {
	t.Helper()

	blockResults, err := predicate.ParseBlockResults(built.EthBlock().Extra())
	require.NoError(t, err)

	txBits := blockResults.Get(validateTx.Hash(), warpcontract.ContractAddress)
	if expectValid {
		require.Zero(t, txBits.Len())
		return
	}

	require.Equal(t, 1, txBits.Len())
	require.True(t, txBits.Contains(0))
}

func (s *SUT) newUnsignedWarpMessage(t *testing.T, payload []byte) *avalancheWarp.UnsignedMessage {
	t.Helper()

	message, err := avalancheWarp.NewUnsignedMessage(s.snowCtx.NetworkID, s.snowCtx.ChainID, payload)
	require.NoError(t, err)
	return message
}

// warpTx returns a tx calling the Warp precompile with txPayload, carrying
// signedMessage, if non-nil, as its predicate.
func warpTx(txPayload []byte, signedMessage *avalancheWarp.Message) *types.DynamicFeeTx {
	var accessList types.AccessList
	if signedMessage != nil {
		accessList = types.AccessList{{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(signedMessage.Bytes()),
		}}
	}

	// Predicate-carrying txs pay the warp signature-verification cost as
	// intrinsic gas, so the limit must cover well over
	// GasCostPerSignatureVerification. Mirrors the legacy plugin's warp tests.
	tx := callTx(warpcontract.ContractAddress, txPayload, 1_000_000)
	tx.AccessList = accessList
	return tx
}

// verifyWarpMessage requests a signature of msg from the SUT and asserts that
// it is refused with the expected error code, or granted if expected is 0.
func (s *SUT) verifyWarpMessage(ctx context.Context, tb testing.TB, msg []byte, expected int32) {
	tb.Helper()

	requestBytes, err := proto.Marshal(&sdk.SignatureRequest{Message: msg})
	require.NoError(tb, err, "proto.Marshal(SignatureRequest)")
	request := p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes)
	require.NoErrorf(tb, s.AppRequest(ctx, s.p2pclient.NodeID(), 1, time.Time{}, request), "%T.AppRequest()", s.VM)

	_, _, response, appErr := s.p2pclient.Response()
	if expected == 0 {
		require.Nil(tb, appErr, "signature request refused")
		// The signature itself is out of scope.
		require.NotNil(tb, response, "signature response")
		return
	}
	require.Nil(tb, response, "signature response")
	require.NotNil(tb, appErr, "signature request refusal")
	require.Equal(tb, expected, appErr.Code, "refusal error code")
}

// TestLegacyVMParsesIglooBlocks checks that the legacy VM MUST parse blocks
// built by this VM, since both VMs parse each other's blocks across the
// transition.
func TestLegacyVMParsesIglooBlocks(t *testing.T) {
	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...), withWarpEnabled())
	// Without Warp transactions, the predicate results are shorter than the
	// legacy fee window.
	sut.send(ctx, t, wallet, 0, transferTx(wallet.Addresses()[0], big.NewInt(1)))
	built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))

	legacyCtx := snowtest.Context(t, sut.snowCtx.ChainID)
	legacyCtx.NetworkUpgrades = sut.snowCtx.NetworkUpgrades
	legacy := &evm.VM{}
	require.NoError(t, legacy.Initialize(
		ctx,
		legacyCtx,
		memdb.New(),
		sut.genesis,
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		&enginetest.Sender{T: t},
	))
	t.Cleanup(func() {
		require.NoError(t, legacy.Shutdown(context.WithoutCancel(ctx)))
	})

	parsed, err := legacy.ParseBlock(ctx, built.Bytes())
	require.NoError(t, err)
	require.Equal(t, built.ID(), parsed.ID())
}
