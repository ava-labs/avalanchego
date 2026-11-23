// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"context"
	"encoding/binary"
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/evm/imp/l1s/warp"
	"github.com/ava-labs/avalanchego/vms/evm/imp/vmtest"
	"github.com/ava-labs/avalanchego/vms/evm/imp/warp/warptest"
	"github.com/ava-labs/avalanchego/vms/evm/saetest"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	saewarp "github.com/ava-labs/avalanchego/vms/evm/imp/warp"
	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

// withWarpEnabled enables the Warp precompile at genesis.
func withWarpEnabled() sutOption {
	return withGenesisPrecompiles(extras.Precompiles{
		warpcontract.ConfigKey: warpcontract.NewDefaultConfig(new(testGenesisTime)),
	})
}

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
	valid := bls.Verify(s.snowCtx.PublicKey, sig, msg.Bytes())
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
	require.ErrorIsf(tb, appErr, want, "reason to refuse signing %s", msg.ID())
}

// newUnsignedWarpMessage wraps b in an unsigned warp message sourced from the
// SUT's chain.
func (s *SUT) newUnsignedWarpMessage(tb testing.TB, b []byte) *avalanchewarp.UnsignedMessage {
	tb.Helper()

	msg, err := avalanchewarp.NewUnsignedMessage(s.snowCtx.NetworkID, s.snowCtx.ChainID, b)
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

// TestSendWarpMessage asserts that the SUT MUST sign a sent warp message, and
// the hash of its block, only once the block is executed.
func TestSendWarpMessage(t *testing.T) {
	wallet := newWallet(t, 1)
	sender := wallet.Addresses()[0]
	ctx, sut := newSUT(t, withMaxAllocFor(sender), withWarpEnabled())

	payload := utils.RandomBytes(100)
	callData, err := warpcontract.PackSendWarpMessage(payload)
	require.NoError(t, err, "warpcontract.PackSendWarpMessage(...)")
	sut.send(ctx, t, wallet.SetNonceAndSign(t, 0, callTx(warpcontract.ContractAddress, callData, 1_000_000)))
	built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))

	sentMsg := sut.newAddressedCallMessage(t, sender.Bytes(), payload)
	hashMsg := sut.newHashMessage(t, built.ID())

	sut.assertWarpSigningRefusal(ctx, t, sentMsg, warp.SourceAddressErrCode)
	sut.assertWarpSigningRefusal(ctx, t, hashMsg, saewarp.NotAcceptedErrCode)

	sut.AcceptAndExecute(ctx, t, built)
	sut.signAndVerifyWarpMessage(ctx, t, sentMsg)
	sut.signAndVerifyWarpMessage(ctx, t, hashMsg)
}

// TestReceiveWarpMessage asserts that the warp precompile MUST deliver each
// predicate message, and its validity, to the tx carrying it.
func TestReceiveWarpMessage(t *testing.T) {
	const numTests = 4
	wallet := newWallet(t, numTests)
	warpLogger := common.Address{'l', 'o', 'g', 'g', 'e', 'r'}
	ctx, sut := newSUT(t,
		withMaxAllocFor(wallet.Addresses()...),
		withWarpEnabled(),
		withAccount(warpLogger, types.Account{Code: saetest.ForwardAndLogCode(t, warpcontract.ContractAddress)}),
	)

	getMessage, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err, "warpcontract.PackGetVerifiedWarpMessage(...)")
	getHash, err := warpcontract.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err, "warpcontract.PackGetVerifiedWarpBlockHash(...)")

	var (
		sourceAddress    = common.Address{1, 2, 3}
		payload          = []byte{4, 5, 6}
		addressedCallMsg = sut.newAddressedCallMessage(t, sourceAddress[:], payload)

		blkID        = ids.GenerateTestID()
		blockHashMsg = sut.newHashMessage(t, blkID)
	)

	must := func(b []byte, err error) []byte {
		require.NoError(t, err)
		return b
	}
	tests := [numTests]struct {
		name        string
		msg         *avalanchewarp.Message
		callData    []byte
		wantLogData []byte
	}{
		{
			name:     "valid_message",
			msg:      sut.validators.Sign(t, addressedCallMsg),
			callData: getMessage,
			wantLogData: must(warpcontract.PackGetVerifiedWarpMessageOutput(warpcontract.GetVerifiedWarpMessageOutput{
				Message: warpcontract.WarpMessage{
					SourceChainID:       common.Hash(sut.snowCtx.ChainID),
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
			wantLogData: must(warpcontract.PackGetVerifiedWarpMessageOutput(warpcontract.GetVerifiedWarpMessageOutput{
				Valid: false,
			})),
		},
		{
			name:     "valid_hash",
			msg:      sut.validators.Sign(t, blockHashMsg),
			callData: getHash,
			wantLogData: must(warpcontract.PackGetVerifiedWarpBlockHashOutput(warpcontract.GetVerifiedWarpBlockHashOutput{
				WarpBlockHash: warpcontract.WarpBlockHash{
					SourceChainID: common.Hash(sut.snowCtx.ChainID),
					BlockHash:     common.Hash(blkID),
				},
				Valid: true,
			})),
		},
		{
			name:     "invalid_hash",
			msg:      warptest.IncorrectlySign(t, blockHashMsg),
			callData: getHash,
			wantLogData: must(warpcontract.PackGetVerifiedWarpBlockHashOutput(warpcontract.GetVerifiedWarpBlockHashOutput{
				Valid: false,
			})),
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			unsigned := callTx(warpLogger, tt.callData, 1_000_000)
			unsigned.AccessList = warptest.AccessList(tt.msg)
			built := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, i, unsigned), vmtest.WithBlockContext(&block.Context{}))

			receipts := built.Receipts()
			require.Lenf(t, receipts, 1, "%T.Receipts()", built)
			require.Lenf(t, receipts[0].Logs, 1, "%T.Logs", receipts[0])
			require.Equal(t, tt.wantLogData, receipts[0].Logs[0].Data, "warp precompile output")
		})
	}
}

// TestInactiveWarpAddressLogsNotSigned asserts that, while the Warp precompile
// is inactive, logs emitted from the Warp address MUST NOT become signable
// messages, even when they are well-formed SendWarpMessage events.
func TestInactiveWarpAddressLogsNotSigned(t *testing.T) {
	// Claims a source address that never sent it.
	forgedPayload, err := payload.NewAddressedCall(utils.RandomBytes(20), utils.RandomBytes(32))
	require.NoError(t, err, "payload.NewAddressedCall(...)")
	chainID := ids.GenerateTestID()
	forged, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, chainID, forgedPayload.Bytes())
	require.NoError(t, err, "avalanchewarp.NewUnsignedMessage(...)")

	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t,
		withChainID(chainID),
		withMaxAllocFor(wallet.Addresses()...),
		withAccount(warpcontract.ContractAddress, types.Account{
			Code: sendWarpMessageLogCode(t, forged),
		}),
	)

	built := sut.buildBlock(ctx, t, blockBuildAdvance, wallet.SetNonceAndSign(t, 0, callTx(warpcontract.ContractAddress, nil, 1_000_000)))
	receipts := built.Receipts()
	require.Lenf(t, receipts, 1, "%T.Receipts()", built)
	require.Lenf(t, receipts[0].Logs, 1, "%T.Logs", receipts[0])
	sut.assertWarpSigningRefusal(ctx, t, forged, warp.SourceAddressErrCode)
}

// sendWarpMessageLogCode returns runtime bytecode that emits the
// SendWarpMessage log the Warp precompile would emit for msg.
func sendWarpMessageLogCode(tb testing.TB, msg *avalanchewarp.UnsignedMessage) []byte {
	tb.Helper()

	topics, data, err := warpcontract.PackSendWarpMessageEvent(common.Address{}, common.Hash(msg.ID()), msg.Bytes())
	require.NoError(tb, err, "warpcontract.PackSendWarpMessageEvent(...)")
	require.Lenf(tb, topics, 3, "warpcontract.PackSendWarpMessageEvent(...) topics")

	push2 := func(v int) []byte {
		return saetest.Push(tb, binary.BigEndian.AppendUint16(nil, uint16(v))) //#nosec G115 -- Test data is far below 64KiB
	}
	// The code copies data, appended after it, into memory and logs it. Its
	// length doesn't depend on dataOffset.
	code := func(dataOffset int) []byte {
		return slices.Concat(
			push2(len(data)), push2(dataOffset), saetest.Ops(vm.PUSH0, vm.CODECOPY), // mem[0:len) = data
			saetest.Push(tb, topics[2][:]),
			saetest.Push(tb, topics[1][:]),
			saetest.Push(tb, topics[0][:]),
			push2(len(data)), saetest.Ops(vm.PUSH0, vm.LOG3, vm.STOP),
		)
	}
	return slices.Concat(code(len(code(0))), data)
}

// TestLegacyVMParsesIglooBlocks asserts that the legacy VM MUST parse blocks
// built by this VM, since both VMs parse each other's blocks across the
// transition.
func TestLegacyVMParsesIglooBlocks(t *testing.T) {
	wallet := newWallet(t, 1)
	ctx, sut := newSUT(t, withMaxAllocFor(wallet.Addresses()...), withWarpEnabled())
	// Without Warp transactions, the predicate results are shorter than the
	// legacy fee window.
	sut.send(ctx, t, wallet.SetNonceAndSign(t, 0, transferTx(common.Address{1}, common.Big1)))
	built := sut.BuildVerify(ctx, t, sut.LastAcceptedID(ctx, t))

	legacyCtx := snowtest.Context(t, sut.snowCtx.ChainID)
	legacyCtx.NetworkUpgrades = sut.snowCtx.NetworkUpgrades
	legacy := &evm.VM{}
	require.NoErrorf(t, legacy.Initialize(
		ctx,
		legacyCtx,
		memdb.New(),
		sut.genesis,
		nil, // upgradeBytes
		nil, // configBytes
		nil, // fxs
		&enginetest.Sender{T: t},
	), "%T.Initialize()", legacy)
	t.Cleanup(func() {
		require.NoErrorf(t, legacy.Shutdown(context.WithoutCancel(ctx)), "%T.Shutdown()", legacy)
	})

	parsed, err := legacy.ParseBlock(ctx, built.Bytes())
	require.NoErrorf(t, err, "%T.ParseBlock()", legacy)
	require.Equal(t, built.ID(), parsed.ID(), "parsed block ID")
}
