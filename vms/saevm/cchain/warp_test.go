// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/ids"
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
)

// signWarpMessage requests the SUT to sign msg, verifies the signature, and
// returns the signature.
func (s *SUT) signWarpMessage(
	ctx context.Context,
	tb testing.TB,
	msg *avalanchewarp.UnsignedMessage,
) *bls.Signature {
	tb.Helper()

	var response sdk.SignatureResponse
	appErr := s.appRequest(
		ctx,
		tb,
		acp118.HandlerID,
		&sdk.SignatureRequest{
			Message: msg.Bytes(),
		},
		&response,
	)
	require.Nilf(tb, appErr, "signing %s", msg.ID())

	sig, err := bls.SignatureFromBytes(response.Signature)
	require.NoError(tb, err, "bls.SignatureFromBytes(...)")
	valid := bls.Verify(s.ctx.PublicKey, sig, msg.Bytes())
	require.Truef(tb, valid, "bls.Verify(signature over %s)", msg.ID())
	return sig
}

// assertWarpSigningRefusal asserts that the SUT refuses, with the given error
// code, to sign msg.
func (s *SUT) assertWarpSigningRefusal(
	ctx context.Context,
	tb testing.TB,
	msg *avalanchewarp.UnsignedMessage,
	wantErrCode int32,
) {
	tb.Helper()

	appErr := s.appRequest(
		ctx,
		tb,
		acp118.HandlerID,
		&sdk.SignatureRequest{
			Message: msg.Bytes(),
		},
		&sdk.SignatureResponse{},
	)
	require.NotNilf(tb, appErr, "signing %s should be refused", msg.ID())

	want := &snowcommon.AppError{
		Code: wantErrCode,
	}
	assert.ErrorIsf(tb, appErr, want, "reason to refuse signing %s", msg.ID())
}

// newUnsignedWarpMessage wraps payloadBytes in an unsigned warp message sourced
// from the SUT's chain.
func (s *SUT) newUnsignedWarpMessage(tb testing.TB, payloadBytes []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	msg, err := avalanchewarp.NewUnsignedMessage(s.ctx.NetworkID, s.ctx.ChainID, payloadBytes)
	require.NoError(tb, err, "avalanchewarp.NewUnsignedMessage(...)")
	return msg
}

// newAddressedCallMessage returns an unsigned warp message, sourced from the
// SUT's chain, with a [payload.AddressedCall] payload.
func (s *SUT) newAddressedCallMessage(tb testing.TB, sourceAddress, payloadBytes []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	p, err := payload.NewAddressedCall(sourceAddress, payloadBytes)
	require.NoError(tb, err, "payload.NewAddressedCall(...)")
	return s.newUnsignedWarpMessage(tb, p.Bytes())
}

// newBlockHashMessage returns an unsigned warp message, sourced from the SUT's
// chain, with a [payload.Hash] payload committing to blkID.
func (s *SUT) newBlockHashMessage(tb testing.TB, blkID ids.ID) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	p, err := payload.NewHash(blkID)
	require.NoError(tb, err, "payload.NewHash(...)")
	return s.newUnsignedWarpMessage(tb, p.Bytes())
}

// assertPredicateResult asserts that the predicate results committed to blk's
// header record warpTx's predicate as valid or invalid.
func (s *SUT) assertPredicateResult(
	tb testing.TB,
	blk *blocks.Block,
	warpTx *types.Transaction,
	wantValid bool,
) {
	tb.Helper()

	ethBlock := blk.EthBlock()
	chainConfig := s.GethRPCBackends().ChainConfig()
	rules := chainConfig.Rules(ethBlock.Number(), params.IsMergeTODO, ethBlock.Time())
	predicateBytes := customheader.PredicateBytesFromExtra(
		params.GetRulesExtra(rules).AvalancheRules,
		ethBlock.Extra(),
	)
	results, err := predicate.ParseBlockResults(predicateBytes)
	require.NoError(tb, err, "predicate.ParseBlockResults(...)")

	// A set bit marks the index of a predicate that failed verification;
	// these tests attach at most one predicate per tx.
	failed := results.Get(warpTx.Hash(), corethwarp.ContractAddress)
	if wantValid {
		assert.Zerof(tb, failed.Len(), "failed-predicate bits of tx %s", warpTx.Hash())
		return
	}
	assert.Equalf(tb, 1, failed.Len(), "failed-predicate bits of tx %s", warpTx.Hash())
	assert.Truef(tb, failed.Contains(0), "failed-predicate bit 0 of tx %s", warpTx.Hash())
}

// TestSendWarpMessage verifies that a warp message emitted by the warp
// precompile is only available for signing after the block containing the
// emitting tx has been accepted and executed.
func TestSendWarpMessage(t *testing.T) {
	var (
		ethWallet = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		sender    = ethWallet.Addresses()[0]
	)
	ctx, sut := newSUT(t, withMaxAllocFor(sender))

	payloadData := utils.RandomBytes(100)
	sendInput, err := corethwarp.PackSendWarpMessage(payloadData)
	require.NoError(t, err, "corethwarp.PackSendWarpMessage(...)")
	warpTx := ethWallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &corethwarp.ContractAddress,
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(1),
		Data:      sendInput,
	})
	require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, warpTx), "%T.SendTransaction(...)", sut.ethclient)

	built := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t))
	if diff := cmp.Diff(types.Transactions{warpTx}, built.Transactions(), cmputils.TransactionsByHash()); diff != "" {
		t.Errorf("%T eth txs (-want +got):\n%s", built, diff)
	}

	// Until the block has been accepted and executed, neither the emitted
	// message nor the block hash may be signed.
	sentMsg := sut.newAddressedCallMessage(t, sender.Bytes(), payloadData)
	sut.assertWarpSigningRefusal(ctx, t, sentMsg, warp.UnknownMessageErrCode)
	blockHashMsg := sut.newBlockHashMessage(t, built.ID())
	sut.assertWarpSigningRefusal(ctx, t, blockHashMsg, warp.NotAcceptedErrCode)

	sut.acceptAndExecute(ctx, t, built)

	// Execution produces the receipt carrying the SendWarpMessage log.
	receipts := built.Receipts()
	require.Lenf(t, receipts, 1, "%T.Receipts()", built)
	logs := receipts[0].Logs
	require.Lenf(t, logs, 1, "%T.Logs", receipts[0])
	wantTopics := []common.Hash{
		corethwarp.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(sender.Bytes()),
		common.Hash(sentMsg.ID()),
	}
	assert.Equalf(t, wantTopics, logs[0].Topics, "%T.Topics", logs[0])

	loggedMsg, err := corethwarp.UnpackSendWarpEventDataToMessage(logs[0].Data)
	require.NoError(t, err, "corethwarp.UnpackSendWarpEventDataToMessage(...)")
	assert.Equalf(t, sentMsg, loggedMsg, "%T.Data as warp message", logs[0])

	// Both messages are now available for signing.
	sut.signWarpMessage(ctx, t, sentMsg)
	sut.signWarpMessage(ctx, t, blockHashMsg)
}

// warpAccessList returns an access list carrying each of messages for the warp
// precompile.
func warpAccessList(msgs ...*avalanchewarp.Message) types.AccessList {
	al := make(types.AccessList, len(msgs))
	for i, msg := range msgs {
		al[i] = types.AccessTuple{
			Address:     corethwarp.ContractAddress,
			StorageKeys: predicate.New(msg.Bytes()),
		}
	}
	return al
}

// TestPredicateVerification drives warp messages through the predicate path:
// each tx carries a signed message in its access list, and the result of
// verifying the signature is recorded in the block header's predicate results.
func TestPredicateVerification(t *testing.T) {
	var (
		ethWallet = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		sender    = ethWallet.Addresses()[0]
		vdrs      = warptest.NewValidators(t, 2)
	)
	ctx, sut := newSUT(t, withMaxAllocFor(sender), withValidators(vdrs))

	var (
		addressedCallMsg = sut.newAddressedCallMessage(t, sender.Bytes(), []byte{1, 2, 3})
		blockHashMsg     = sut.newBlockHashMessage(t, ids.GenerateTestID())
	)
	getMessageInput, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpMessage(...)")
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
			warpTx := ethWallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:         &corethwarp.ContractAddress,
				Gas:        1_000_000,
				GasFeeCap:  big.NewInt(1),
				Data:       tt.txInput,
				AccessList: warpAccessList(tt.msg),
			})
			require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, warpTx), "%T.SendTransaction(...)", sut.ethclient)

			// Txs carrying predicates can only be verified with a non-nil
			// [block.Context].
			built := sut.buildVerify(
				ctx,
				t,
				sut.lastAccepted(ctx, t),
				withBlockContext(&block.Context{}),
			)
			if diff := cmp.Diff(types.Transactions{warpTx}, built.Transactions(), cmputils.TransactionsByHash()); diff != "" {
				t.Errorf("%T eth txs (-want +got):\n%s", built, diff)
			}

			sut.acceptAndExecute(ctx, t, built)
			sut.assertPredicateResult(t, built, warpTx, tt.wantValid)

			// The tx succeeds either way; a failed predicate is reported to the
			// precompile, not treated as an execution failure.
			receipts := built.Receipts()
			require.Lenf(t, receipts, 1, "%T.Receipts()", built)
			assert.Equalf(t, types.ReceiptStatusSuccessful, receipts[0].Status, "%T.Status", receipts[0])
		})
	}
}
