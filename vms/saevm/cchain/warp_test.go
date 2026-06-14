// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"math/big"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
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
	sut.signWarpMessage(ctx, t, sentMsg)
	sut.signWarpMessage(ctx, t, hashMsg)
}

// forwardAndLogCode returns runtime bytecode that forwards its calldata to
// callee and emits the returned data as a log.
func forwardAndLogCode(callee common.Address) []byte {
	return slices.Concat(
		[]byte{
			// mem[0:cds) = calldata
			byte(vm.CALLDATASIZE), byte(vm.PUSH0), byte(vm.PUSH0), byte(vm.CALLDATACOPY),
			// call callee with mem[0:cds) as input
			byte(vm.PUSH0), byte(vm.PUSH0), // return size + offset; read via RETURNDATACOPY instead
			byte(vm.CALLDATASIZE), byte(vm.PUSH0), // input size + offset
			byte(vm.PUSH0), // value
			byte(vm.PUSH20),
		},
		callee[:],
		[]byte{
			byte(vm.GAS),
			byte(vm.CALL),
			byte(vm.POP), // discard the success flag; a failed call logs empty data
			// mem[0:rds) = return data
			byte(vm.RETURNDATASIZE), byte(vm.PUSH0), byte(vm.PUSH0), byte(vm.RETURNDATACOPY),
			// log0(mem[0:rds))
			byte(vm.RETURNDATASIZE), byte(vm.PUSH0), byte(vm.LOG0),
			byte(vm.STOP),
		},
	)
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

// TestReceiveWarpMessage verifies that warp messages are correctly verified
// delivered by the warp precompile.
func TestReceiveWarpMessage(t *testing.T) {
	var (
		wallet     = saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
		sender     = wallet.Addresses()[0]
		vdrs       = warptest.NewValidators(t, 2)
		warpLogger = common.Address{'l', 'o', 'g', 'g', 'e', 'r'}
	)
	ctx, sut := newSUT(t,
		withMaxAllocFor(sender),
		withContract(warpLogger, forwardAndLogCode(corethwarp.ContractAddress)),
		withValidators(vdrs),
	)

	var (
		sourceAddress    = common.Address{1, 2, 3}
		payload          = []byte{4, 5, 6}
		addressedCallMsg = sut.newAddressedCallMessage(t, sourceAddress[:], payload)
	)
	getMessage, err := corethwarp.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpMessage(...)")

	var (
		blkID        = ids.GenerateTestID()
		blockHashMsg = sut.newHashMessage(t, blkID)
	)
	getHash, err := corethwarp.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err, "corethwarp.PackGetVerifiedWarpBlockHash(...)")

	must := func(b []byte, err error) []byte {
		require.NoError(t, err)
		return b
	}
	tests := []struct {
		name     string
		msg      *avalanchewarp.Message
		callData []byte
		want     []byte
	}{
		{
			name:     "valid_message",
			msg:      vdrs.Sign(t, addressedCallMsg),
			callData: getMessage,
			want: must(corethwarp.PackGetVerifiedWarpMessageOutput(corethwarp.GetVerifiedWarpMessageOutput{
				Message: corethwarp.WarpMessage{
					SourceChainID:       common.Hash(sut.ctx.ChainID),
					OriginSenderAddress: sourceAddress,
					Payload:             payload,
				},
				Valid: true,
			})),
		},
		{
			name:     "invalid_message",
			msg:      warptest.IncorrectlySign(t, addressedCallMsg),
			callData: getMessage,
			want: must(corethwarp.PackGetVerifiedWarpMessageOutput(corethwarp.GetVerifiedWarpMessageOutput{
				Valid: false,
			})),
		},
		{
			name:     "valid_hash",
			msg:      vdrs.Sign(t, blockHashMsg),
			callData: getHash,
			want: must(corethwarp.PackGetVerifiedWarpBlockHashOutput(corethwarp.GetVerifiedWarpBlockHashOutput{
				WarpBlockHash: corethwarp.WarpBlockHash{
					SourceChainID: common.Hash(sut.ctx.ChainID),
					BlockHash:     common.Hash(blkID),
				},
				Valid: true,
			})),
		},
		{
			name:     "invalid_hash",
			msg:      warptest.IncorrectlySign(t, blockHashMsg),
			callData: getHash,
			want: must(corethwarp.PackGetVerifiedWarpBlockHashOutput(corethwarp.GetVerifiedWarpBlockHashOutput{
				Valid: false,
			})),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tx := wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
				To:         &warpLogger,
				Gas:        1_000_000,
				GasFeeCap:  big.NewInt(1),
				Data:       tt.callData,
				AccessList: warpAccessList(tt.msg),
			})
			built := sut.issueEthAndExecute(ctx, t, tx, withBlockContext(&block.Context{}))

			// The warp logger emitted a single log with the warp precompile's
			// response.
			receipts := built.Receipts()
			require.Lenf(t, receipts, 1, "%T.Receipts()", built)
			receipt := receipts[0]
			assert.Equalf(t, types.ReceiptStatusSuccessful, receipt.Status, "%T.Status", receipt)
			logs := receipt.Logs
			require.Lenf(t, logs, 1, "%T.Logs", receipt)
			log := logs[0]
			assert.Equalf(t, tt.want, log.Data, "%T.Data (warp precompile output)", log)
		})
	}
}
