// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/saevm/cmputils"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	corethwarp "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// signAndVerifyWarpMessage requests the SUT to sign msg, verifies the
// signature, and returns the signature.
func (s *SUT) signAndVerifyWarpMessage(
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
	require.Nilf(tb, appErr, "%T.appRequest(%d, %s)", s, acp118.HandlerID, msg.ID())

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

// newUnsignedWarpMessage wraps b in an unsigned warp message sourced from the
// SUT's chain.
func (s *SUT) newUnsignedWarpMessage(tb testing.TB, b []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	msg, err := avalanchewarp.NewUnsignedMessage(s.ctx.NetworkID, s.ctx.ChainID, b)
	require.NoError(tb, err, "avalanchewarp.NewUnsignedMessage(...)")
	return msg
}

// newAddressedCallMessage returns an unsigned warp message, sourced from the
// SUT's chain, with [payload.AddressedCall].
func (s *SUT) newAddressedCallMessage(tb testing.TB, addr, b []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	p, err := payload.NewAddressedCall(addr, b)
	require.NoError(tb, err, "payload.NewAddressedCall(...)")
	return s.newUnsignedWarpMessage(tb, p.Bytes())
}

// newHashMessage returns an unsigned warp message, sourced from the SUT's
// chain, with [payload.Hash] committing to blkID.
func (s *SUT) newHashMessage(tb testing.TB, blkID ids.ID) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	p, err := payload.NewHash(blkID)
	require.NoError(tb, err, "payload.NewHash(...)")
	return s.newUnsignedWarpMessage(tb, p.Bytes())
}

// TestSendWarpMessage verifies that a warp message emitted by the warp
// precompile is correctly signed after the block is executed but not before.
func TestSendWarpMessage(t *testing.T) {
	var (
		wallet = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		sender = wallet.Addresses()[0]
	)
	ctx, sut := newSUT(t, withMaxAllocFor(sender))

	payload := utils.RandomBytes(100)
	callData, err := corethwarp.PackSendWarpMessage(payload)
	require.NoError(t, err, "corethwarp.PackSendWarpMessage(...)")
	tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &corethwarp.ContractAddress,
		Gas:       1_000_000,
		GasFeeCap: big.NewInt(1),
		Data:      callData,
	})
	require.NoErrorf(t, sut.ethclient.SendTransaction(ctx, tx), "%T.SendTransaction(...)", sut.ethclient)

	built := sut.buildVerify(ctx, t, sut.lastAccepted(ctx, t))
	if diff := cmp.Diff(types.Transactions{tx}, built.Transactions(), cmputils.TransactionsByHash()); diff != "" {
		t.Errorf("%T eth txs (-want +got):\n%s", built, diff)
	}

	sentMsg := sut.newAddressedCallMessage(t, sender.Bytes(), payload)
	hashMsg := sut.newHashMessage(t, built.ID())

	sut.assertWarpSigningRefusal(ctx, t, sentMsg, warp.UnknownMessageErrCode)
	sut.assertWarpSigningRefusal(ctx, t, hashMsg, warp.NotAcceptedErrCode)

	sut.acceptAndExecute(ctx, t, built)
	sut.signAndVerifyWarpMessage(ctx, t, sentMsg)
	sut.signAndVerifyWarpMessage(ctx, t, hashMsg)
}

// forwardAndLogCode returns runtime bytecode that forwards its calldata to
// callee and emits the returned data as a log.
func forwardAndLogCode(tb testing.TB, callee common.Address) []byte {
	tb.Helper()
	return slices.Concat(
		// mem[0:cds) = calldata
		saetest.Ops(vm.CALLDATASIZE, vm.PUSH0, vm.PUSH0, vm.CALLDATACOPY),
		// call callee with mem[0:cds) as input
		saetest.Ops(vm.PUSH0, vm.PUSH0),        // return size + offset; read via RETURNDATACOPY instead
		saetest.Ops(vm.CALLDATASIZE, vm.PUSH0), // input size + offset
		saetest.Ops(vm.PUSH0),                  // value
		saetest.Push(tb, callee[:]),
		saetest.Ops(vm.GAS, vm.CALL),
		saetest.Ops(vm.POP), // discard the success flag
		// mem[0:rds) = return data
		saetest.Ops(vm.RETURNDATASIZE, vm.PUSH0, vm.PUSH0, vm.RETURNDATACOPY),
		// log0(mem[0:rds))
		saetest.Ops(vm.RETURNDATASIZE, vm.PUSH0, vm.LOG0),
		saetest.Ops(vm.STOP),
	)
}

// warpAccessList returns an access list carrying each of the messages for the
// warp precompile.
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

// TestReceiveWarpMessage verifies that the intrinsic gas accounts for warp
// messages and that the warp precompile correctly delivers messages.
func TestReceiveWarpMessage(t *testing.T) {
	const numTests = 8
	var (
		wallet     = saetest.NewUNSAFEWallet(t, numTests, types.LatestSigner(saetest.ChainConfig()))
		vdrs       = warptest.NewValidators(t, 2)
		warpLogger = common.Address{'l', 'o', 'g', 'g', 'e', 'r'}
	)
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withAccount(warpLogger, types.Account{Code: forwardAndLogCode(t, corethwarp.ContractAddress)}),
		withValidators(vdrs),
	)

	getMessage, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpMessage(...)")
	getHash, err := corethwarp.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpBlockHash(...)")

	var (
		sourceAddress    = common.Address{1, 2, 3}
		payload          = []byte{4, 5, 6}
		addressedCallMsg = sut.newAddressedCallMessage(t, sourceAddress[:], payload)

		blkID        = ids.GenerateTestID()
		blockHashMsg = sut.newHashMessage(t, blkID)

		emptyMsg = vdrs.Sign(t, sut.newAddressedCallMessage(t, sourceAddress[:], nil))
		largeMsg = vdrs.Sign(t, sut.newAddressedCallMessage(t, sourceAddress[:], make([]byte, 5*units.KiB)))
	)

	// The C-Chain modifies the intrinsic gas of transactions to charge for warp
	// message verification and serialization rather than storage reads. For
	// small messages this increases the intrinsic gas. For large messages this
	// decreases the intrinsic gas.
	const (
		emptyWarpIntrinsicGas = 149_764
		largeWarpIntrinsicGas = 231_684
		e2eGas                = 1_000_000 // Enough for the call to succeed
	)
	must := func(b []byte, err error) []byte {
		require.NoError(t, err)
		return b
	}
	tests := [numTests]struct {
		name         string
		msg          *avalanchewarp.Message
		callData     []byte
		gas          uint64
		wantIssueErr testerr.Want
		wantStatus   uint64
		wantLogData  []byte
	}{
		{
			name:         "empty_message_underfunded",
			msg:          emptyMsg,
			callData:     getMessage,
			gas:          emptyWarpIntrinsicGas - 1,
			wantIssueErr: testerr.Contains(core.ErrIntrinsicGas.Error()),
		},
		{
			name:       "empty_message_minimally_funded",
			msg:        emptyMsg,
			callData:   getMessage,
			gas:        emptyWarpIntrinsicGas,
			wantStatus: types.ReceiptStatusFailed,
		},
		{
			name:         "large_message_underfunded",
			msg:          largeMsg,
			callData:     getMessage,
			gas:          largeWarpIntrinsicGas - 1,
			wantIssueErr: testerr.Contains(core.ErrIntrinsicGas.Error()),
		},
		{
			name:       "large_message_minimally_funded",
			msg:        largeMsg,
			callData:   getMessage,
			gas:        largeWarpIntrinsicGas,
			wantStatus: types.ReceiptStatusFailed,
		},
		{
			name:       "valid_message",
			msg:        vdrs.Sign(t, addressedCallMsg),
			callData:   getMessage,
			gas:        e2eGas,
			wantStatus: types.ReceiptStatusSuccessful,
			wantLogData: must(corethwarp.PackGetVerifiedWarpMessageOutput(corethwarp.GetVerifiedWarpMessageOutput{
				Message: corethwarp.WarpMessage{
					SourceChainID:       common.Hash(sut.ctx.ChainID),
					OriginSenderAddress: sourceAddress,
					Payload:             payload,
				},
				Valid: true,
			})),
		},
		{
			name:       "invalid_message",
			msg:        warptest.IncorrectlySign(t, addressedCallMsg),
			callData:   getMessage,
			gas:        e2eGas,
			wantStatus: types.ReceiptStatusSuccessful,
			wantLogData: must(corethwarp.PackGetVerifiedWarpMessageOutput(corethwarp.GetVerifiedWarpMessageOutput{
				Valid: false,
			})),
		},
		{
			name:       "valid_hash",
			msg:        vdrs.Sign(t, blockHashMsg),
			callData:   getHash,
			gas:        e2eGas,
			wantStatus: types.ReceiptStatusSuccessful,
			wantLogData: must(corethwarp.PackGetVerifiedWarpBlockHashOutput(corethwarp.GetVerifiedWarpBlockHashOutput{
				WarpBlockHash: corethwarp.WarpBlockHash{
					SourceChainID: common.Hash(sut.ctx.ChainID),
					BlockHash:     common.Hash(blkID),
				},
				Valid: true,
			})),
		},
		{
			name:       "invalid_hash",
			msg:        warptest.IncorrectlySign(t, blockHashMsg),
			callData:   getHash,
			gas:        e2eGas,
			wantStatus: types.ReceiptStatusSuccessful,
			wantLogData: must(corethwarp.PackGetVerifiedWarpBlockHashOutput(corethwarp.GetVerifiedWarpBlockHashOutput{
				Valid: false,
			})),
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := wallet.SetNonceAndSign(t, i, &types.DynamicFeeTx{
				To:         &warpLogger,
				Gas:        tt.gas,
				GasFeeCap:  big.NewInt(1),
				Data:       tt.callData,
				AccessList: warpAccessList(tt.msg),
			})
			err := sut.ethclient.SendTransaction(ctx, tx)
			if diff := testerr.Diff(err, tt.wantIssueErr); diff != "" {
				t.Fatalf("%T.SendTransaction(...) error (-want +got)\n%s", sut.ethclient, diff)
			}
			if err != nil {
				return
			}

			sut.waitForPendingEthTxs(ctx, t, tx)
			built := sut.runConsensusLoop(ctx, t, withBlockContext(&block.Context{}))
			receipts := built.Receipts()
			require.Lenf(t, receipts, 1, "%T.Receipts()", built)
			receipt := receipts[0]
			assert.Equalf(t, tt.wantStatus, receipt.Status, "%T.Status", receipt)
			if receipt.Status != types.ReceiptStatusSuccessful {
				return
			}

			log := saetest.SoleLog(t, receipt)
			assert.Equalf(t, tt.wantLogData, log.Data, "%T.Data (warp precompile output)", log)
		})
	}
}
