// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

// TestSendWarpMessage verifies that a warp message emitted by a tx is only
// available for signing AFTER the block containing the tx has executed.
func TestSendWarpMessage(t *testing.T) {
	ethWallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := ethWallet.Addresses()[0]

	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sender)
	}))
	capturer := saetest.NewCapturingPeer(t, set.Set[ids.NodeID]{})
	saetest.ConnectTo[saetest.Peer](t, sut, capturer)

	payloadData := utils.RandomBytes(100)
	warpSendInput, err := warpcontract.PackSendWarpMessage(payloadData)
	require.NoError(t, err)

	sendWarpTx(ctx, t, sut, ethWallet, warpSendInput, nil /*no predicate*/)

	built := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t))
	require.Len(t, built.EthBlock().Transactions(), 1)

	// Before the block has executed the warp verifier has nothing to sign.
	addressedPayload, err := payload.NewAddressedCall(sender.Bytes(), payloadData)
	require.NoError(t, err)
	unsignedMessage := newUnsignedWarpMessage(t, sut, addressedPayload.Bytes())
	verifyWarpMessage(ctx, t, sut, capturer, unsignedMessage.Bytes(), warp.UnknownMessageErrCode)

	blockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(t, err)
	blockMessage := newUnsignedWarpMessage(t, sut, blockHashPayload.Bytes())
	verifyWarpMessage(ctx, t, sut, capturer, blockMessage.Bytes(), warp.NotAcceptedErrCode)

	sut.acceptAndExecute(ctx, t, built)

	// Receipts are produced once the block has executed.
	receipts := built.Receipts()
	require.Len(t, receipts, 1)
	require.Len(t, receipts[0].Logs, 1)
	wantTopics := []common.Hash{
		warpcontract.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(sender.Bytes()),
		common.Hash(unsignedMessage.ID()),
	}
	require.Equal(t, wantTopics, receipts[0].Logs[0].Topics)

	loggedMessage, err := warpcontract.UnpackSendWarpEventDataToMessage(receipts[0].Logs[0].Data)
	require.NoError(t, err)
	require.Equal(t, unsignedMessage, loggedMessage)

	// Both messages should now verify cleanly.
	verifyWarpMessage(ctx, t, sut, capturer, unsignedMessage.Bytes(), 0)
	verifyWarpMessage(ctx, t, sut, capturer, blockMessage.Bytes(), 0)
}

// TestPredicateVerification drives warp messages through the predicate path:
// the tx carries the (signed) message in its access list, and the result is
// recorded in the block header's predicate-results extra.
func TestPredicateVerification(t *testing.T) {
	ethWallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := ethWallet.Addresses()[0]

	vdrs := warptest.NewValidators(t, 2)
	ctx, sut := newSUT(t,
		options.Func[sutConfig](func(c *sutConfig) {
			c.genesis.Alloc = saetest.MaxAllocFor(sender)
		}),
		withValidators(vdrs),
	)

	addressedPayload, err := payload.NewAddressedCall(sender.Bytes(), []byte{1, 2, 3})
	require.NoError(t, err)
	addressedCallMessage := newUnsignedWarpMessage(t, sut, addressedPayload.Bytes())
	addressedCallTxPayload, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err)

	blockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	blockHashMessage := newUnsignedWarpMessage(t, sut, blockHashPayload.Bytes())
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
			signedMsg:      vdrs.Sign(t, addressedCallMessage),
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
			signedMsg:      vdrs.Sign(t, blockHashMessage),
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
			validateTx := sendWarpTx(ctx, t, sut, ethWallet, tt.txPayload, tt.signedMsg)

			built := sut.buildVerifyWithContext(ctx, t, sut.lastAccepted(ctx, t), &block.Context{PChainHeight: 0})
			require.Len(t, built.EthBlock().Transactions(), 1)

			sut.acceptAndExecute(ctx, t, built)
			assertPredicateResult(t, sut, built, validateTx, tt.validPredicate)

			receipts := built.Receipts()
			require.Len(t, receipts, 1)
			require.Equal(t, types.ReceiptStatusSuccessful, receipts[0].Status)
		})
	}
}

// sendWarpTx submits a tx that calls the warp precompile. If signedMessage is
// non-nil it is attached to the tx access list as a predicate input.
func sendWarpTx(
	ctx context.Context,
	t *testing.T,
	sut *SUT,
	w *saetest.Wallet,
	txPayload []byte,
	signedMessage *avalancheWarp.Message,
) *types.Transaction {
	t.Helper()

	var accessList types.AccessList
	if signedMessage != nil {
		accessList = types.AccessList{{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(signedMessage.Bytes()),
		}}
	}

	warpAddr := warpcontract.ContractAddress
	signedTx := w.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:         &warpAddr,
		Gas:        1_000_000,
		GasFeeCap:  big.NewInt(225 * ethparams.GWei),
		Value:      big.NewInt(0),
		Data:       txPayload,
		AccessList: accessList,
	})
	require.NoError(t, sut.ethclient.SendTransaction(ctx, signedTx))
	return signedTx
}

// newUnsignedWarpMessage wraps payload into an unsigned warp message for the
// SUT's chain.
func newUnsignedWarpMessage(t *testing.T, sut *SUT, payload []byte) *avalancheWarp.UnsignedMessage {
	t.Helper()
	msg, err := avalancheWarp.NewUnsignedMessage(sut.ctx.NetworkID, sut.ctx.ChainID, payload)
	require.NoError(t, err)
	return msg
}

// verifyWarpMessage sends an acp118 signature request to the SUT and asserts
// that the response (delivered to capturer) matches the expected error code.
// expected == 0 means "valid message, response is non-nil and error is nil".
func verifyWarpMessage(
	ctx context.Context,
	t *testing.T,
	sut *SUT,
	capturer *saetest.CapturingPeer,
	payloadBytes []byte,
	expected int32,
) {
	t.Helper()

	req := &sdk.SignatureRequest{Message: payloadBytes}
	requestBytes, err := proto.Marshal(req)
	require.NoError(t, err)

	msg := p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes)
	deadline, _ := ctx.Deadline()
	require.NoError(t, sut.AppRequest(ctx, capturer.NodeID(), 1, deadline, msg))

	_, _, got, appErr := capturer.Response()
	if expected == 0 {
		require.NotNil(t, got)
		return
	}
	require.Nil(t, got)
	require.NotNil(t, appErr)
	require.Equal(t, expected, appErr.Code)
}

// assertPredicateResult parses the predicate result extra from blk's header
// and asserts that validateTx's bit corresponds to expectValid.
func assertPredicateResult(
	t *testing.T,
	sut *SUT,
	blk *blocks.Block,
	validateTx *types.Transaction,
	expectValid bool,
) {
	t.Helper()

	chainConfig := sut.GethRPCBackends().ChainConfig()
	rules := chainConfig.Rules(blk.EthBlock().Number(), params.IsMergeTODO, blk.EthBlock().Time())
	rulesExtra := params.GetRulesExtra(rules)
	headerPredicateBytes := customheader.PredicateBytesFromExtra(
		rulesExtra.AvalancheRules,
		blk.EthBlock().Extra(),
	)
	blockResults, err := predicate.ParseBlockResults(headerPredicateBytes)
	require.NoError(t, err)

	txBits := blockResults.Get(validateTx.Hash(), warpcontract.ContractAddress)
	if expectValid {
		require.Zero(t, txBits.Len())
		return
	}
	require.Equal(t, 1, txBits.Len())
	require.True(t, txBits.Contains(0))
}
