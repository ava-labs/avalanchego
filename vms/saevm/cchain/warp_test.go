// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

// TestSendWarpMessage verifies that a warp message emitted by the warp
// precompile is only available for signing after the block containing the
// emitting tx has been accepted and executed.
func TestSendWarpMessage(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := wallet.Addresses()[0]

	ctx, sut := newSUT(t, withMaxAllocFor(sender))
	capturer := saetest.NewCapturingPeer(t, nil /*vdrs*/)
	saetest.ConnectTo[saetest.Peer](t, sut, capturer)

	payloadData := utils.RandomBytes(100)
	sendInput, err := corethwarp.PackSendWarpMessage(payloadData)
	require.NoError(t, err, "corethwarp.PackSendWarpMessage(...)")
	warpTx := sendWarpTx(ctx, t, sut, wallet, sendInput, nil /*signedMsg*/)

	built := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t))
	if diff := cmp.Diff(types.Transactions{warpTx}, built.Transactions(), cmputils.TransactionsByHash()); diff != "" {
		t.Errorf("%T eth txs (-want +got):\n%s", built, diff)
	}

	// Until the block has been accepted and executed, neither the emitted
	// message nor the block hash may be signed.
	addressedPayload, err := payload.NewAddressedCall(sender.Bytes(), payloadData)
	require.NoError(t, err, "payload.NewAddressedCall(...)")
	sentMsg := newUnsignedWarpMessage(t, sut, addressedPayload.Bytes())
	assertWarpNotSigned(ctx, t, sut, capturer, sentMsg, warp.UnknownMessageErrCode)

	blockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(t, err, "payload.NewHash(...)")
	blockHashMsg := newUnsignedWarpMessage(t, sut, blockHashPayload.Bytes())
	assertWarpNotSigned(ctx, t, sut, capturer, blockHashMsg, warp.NotAcceptedErrCode)

	sut.acceptAndExecute(ctx, t, built)

	// Execution produces the receipt carrying the SendWarpMessage log.
	receipts := built.Receipts()
	require.Len(t, receipts, 1)
	logs := receipts[0].Logs
	require.Len(t, logs, 1)
	wantTopics := []common.Hash{
		corethwarp.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(sender.Bytes()),
		common.Hash(sentMsg.ID()),
	}
	require.Equal(t, wantTopics, logs[0].Topics)

	loggedMsg, err := corethwarp.UnpackSendWarpEventDataToMessage(logs[0].Data)
	require.NoError(t, err, "corethwarp.UnpackSendWarpEventDataToMessage(...)")
	require.Equal(t, sentMsg, loggedMsg)

	// Both messages are now available for signing.
	assertWarpSigned(ctx, t, sut, capturer, sentMsg)
	assertWarpSigned(ctx, t, sut, capturer, blockHashMsg)
}

// TestPredicateVerification drives warp messages through the predicate path:
// each tx carries a signed message in its access list, and the result of
// verifying the signature is recorded in the block header's predicate results.
func TestPredicateVerification(t *testing.T) {
	wallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := wallet.Addresses()[0]

	vdrs := warptest.NewValidators(t, 2)
	ctx, sut := newSUT(t, withMaxAllocFor(sender), withValidators(vdrs))

	addressedPayload, err := payload.NewAddressedCall(sender.Bytes(), []byte{1, 2, 3})
	require.NoError(t, err, "payload.NewAddressedCall(...)")
	addressedCallMsg := newUnsignedWarpMessage(t, sut, addressedPayload.Bytes())
	getMessageInput, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpMessage(...)")

	blockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err, "payload.NewHash(...)")
	blockHashMsg := newUnsignedWarpMessage(t, sut, blockHashPayload.Bytes())
	getBlockHashInput, err := corethwarp.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpBlockHash(...)")

	tests := []struct {
		name      string
		msg       *avalanchewarp.Message
		txInput   []byte
		wantValid bool
	}{
		{
			name:      "valid warp message",
			msg:       vdrs.Sign(t, addressedCallMsg),
			txInput:   getMessageInput,
			wantValid: true,
		},
		{
			name:      "invalid warp message",
			msg:       warptest.IncorrectlySign(t, addressedCallMsg),
			txInput:   getMessageInput,
			wantValid: false,
		},
		{
			name:      "valid warp block hash",
			msg:       vdrs.Sign(t, blockHashMsg),
			txInput:   getBlockHashInput,
			wantValid: true,
		},
		{
			name:      "invalid warp block hash",
			msg:       warptest.IncorrectlySign(t, blockHashMsg),
			txInput:   getBlockHashInput,
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warpTx := sendWarpTx(ctx, t, sut, wallet, tt.txInput, tt.msg)

			// Txs carrying predicates can only be verified with a non-nil
			// [block.Context].
			built := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t), withBlockContext(&block.Context{}))
			if diff := cmp.Diff(types.Transactions{warpTx}, built.Transactions(), cmputils.TransactionsByHash()); diff != "" {
				t.Errorf("%T eth txs (-want +got):\n%s", built, diff)
			}

			sut.acceptAndExecute(ctx, t, built)
			assertPredicateResult(t, sut, built, warpTx, tt.wantValid)

			// The tx succeeds either way; a failed predicate is reported to the
			// precompile, not treated as an execution failure.
			receipts := built.Receipts()
			require.Len(t, receipts, 1)
			require.Equal(t, types.ReceiptStatusSuccessful, receipts[0].Status)
		})
	}
}

// sendWarpTx signs and submits a tx calling the warp precompile with input. If
// msg is non-nil, it is attached to the tx's access list as a predicate.
func sendWarpTx(
	ctx context.Context,
	tb testing.TB,
	sut *SUT,
	wallet *saetest.Wallet,
	input []byte,
	msg *avalanchewarp.Message,
) *types.Transaction {
	tb.Helper()

	var accessList types.AccessList
	if msg != nil {
		accessList = types.AccessList{{
			Address:     corethwarp.ContractAddress,
			StorageKeys: predicate.New(msg.Bytes()),
		}}
	}

	contract := corethwarp.ContractAddress
	signedTx := wallet.SetNonceAndSign(tb, 0, &types.DynamicFeeTx{
		To:         &contract,
		Gas:        1_000_000,
		GasFeeCap:  big.NewInt(225 * ethparams.GWei),
		Data:       input,
		AccessList: accessList,
	})
	require.NoErrorf(tb, sut.ethclient.SendTransaction(ctx, signedTx), "%T.SendTransaction(...)", sut.ethclient)
	return signedTx
}

// newUnsignedWarpMessage wraps payloadBytes in an unsigned warp message sourced
// from the SUT's chain.
func newUnsignedWarpMessage(tb testing.TB, sut *SUT, payloadBytes []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	msg, err := avalanchewarp.NewUnsignedMessage(sut.ctx.NetworkID, sut.ctx.ChainID, payloadBytes)
	require.NoError(tb, err, "avalanchewarp.NewUnsignedMessage(...)")
	return msg
}

// requestWarpSignature sends an ACP-118 signature request for msg to the SUT
// and returns the response delivered to capturer.
func requestWarpSignature(
	ctx context.Context,
	tb testing.TB,
	sut *SUT,
	capturer *saetest.CapturingPeer,
	msg *avalanchewarp.UnsignedMessage,
) ([]byte, *snowcommon.AppError) {
	tb.Helper()

	requestBytes, err := proto.Marshal(&sdk.SignatureRequest{Message: msg.Bytes()})
	require.NoErrorf(tb, err, "proto.Marshal(%T)", &sdk.SignatureRequest{})

	request := p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes)
	require.NoErrorf(tb, sut.AppRequest(
		ctx,
		capturer.NodeID(),
		1,           // requestID
		time.Time{}, // deadline; ignored by the ACP-118 handler
		request,
	), "%T.AppRequest()", sut.VM)

	_, _, responseBytes, appErr := capturer.Response()
	return responseBytes, appErr
}

// assertWarpSigned asserts that the SUT signs msg on request and that the
// response carries the SUT's BLS signature over msg.
func assertWarpSigned(
	ctx context.Context,
	tb testing.TB,
	sut *SUT,
	capturer *saetest.CapturingPeer,
	msg *avalanchewarp.UnsignedMessage,
) {
	tb.Helper()

	responseBytes, appErr := requestWarpSignature(ctx, tb, sut, capturer, msg)
	require.Nil(tb, appErr)

	response := &sdk.SignatureResponse{}
	require.NoErrorf(tb, proto.Unmarshal(responseBytes, response), "proto.Unmarshal(%T)", response)

	sig, err := bls.SignatureFromBytes(response.Signature)
	require.NoError(tb, err, "bls.SignatureFromBytes(...)")
	require.True(tb, bls.Verify(sut.ctx.PublicKey, sig, msg.Bytes()), "bls.Verify(...)")
}

// assertWarpNotSigned asserts that the SUT refuses, with the given error code,
// to sign msg.
func assertWarpNotSigned(
	ctx context.Context,
	tb testing.TB,
	sut *SUT,
	capturer *saetest.CapturingPeer,
	msg *avalanchewarp.UnsignedMessage,
	wantErrCode int32,
) {
	tb.Helper()

	_, appErr := requestWarpSignature(ctx, tb, sut, capturer, msg)
	require.NotNil(tb, appErr)
	require.Equal(tb, wantErrCode, appErr.Code)
}

// assertPredicateResult asserts that the predicate results committed to blk's
// header record warpTx's predicate as valid or invalid.
func assertPredicateResult(
	tb testing.TB,
	sut *SUT,
	blk *blocks.Block,
	warpTx *types.Transaction,
	wantValid bool,
) {
	tb.Helper()

	ethBlock := blk.EthBlock()
	chainConfig := sut.GethRPCBackends().ChainConfig()
	rules := chainConfig.Rules(ethBlock.Number(), params.IsMergeTODO, ethBlock.Time())
	predicateBytes := customheader.PredicateBytesFromExtra(
		params.GetRulesExtra(rules).AvalancheRules,
		ethBlock.Extra(),
	)
	results, err := predicate.ParseBlockResults(predicateBytes)
	require.NoError(tb, err, "predicate.ParseBlockResults(...)")

	// A set bit marks the index of a predicate that failed verification.
	failed := results.Get(warpTx.Hash(), corethwarp.ContractAddress)
	if wantValid {
		require.Zero(tb, failed.Len())
		return
	}
	require.Equal(tb, 1, failed.Len())
	require.True(tb, failed.Contains(0))
}
