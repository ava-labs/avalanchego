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
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethparams "github.com/ava-labs/libevm/params"
)

// peer captures AppResponse, AppRequestFailed, and AppGossip messages and
// exposes them for inspection.
type peer struct {
	id       ids.NodeID
	sender   *saetest.Sender
	response *buffer.UnboundedBlockingDeque[peerResponse]
	gossip   *buffer.UnboundedBlockingDeque[peerGossip]
}

type peerResponse struct {
	nodeID    ids.NodeID
	requestID uint32
	bytes     []byte
	err       *snowcommon.AppError
}

type peerGossip struct {
	nodeID ids.NodeID
	bytes  []byte
}

// newPeer returns a [peer] with a fresh NodeID. vdrs is passed through to its
// underlying [saetest.Sender].
func newPeer(tb testing.TB, vdrs set.Set[ids.NodeID]) *peer {
	p := &peer{
		id:       ids.GenerateTestNodeID(),
		sender:   saetest.NewSender(tb, vdrs),
		response: buffer.NewUnboundedBlockingDeque[peerResponse](1),
		gossip:   buffer.NewUnboundedBlockingDeque[peerGossip](1),
	}
	p.sender.SetSelf(p)
	return p
}

// Response blocks until the peer captures an AppResponse or AppRequestFailed to
// return.
func (p *peer) Response() (ids.NodeID, uint32, []byte, *snowcommon.AppError) {
	r, _ := p.response.PopLeft()
	return r.nodeID, r.requestID, r.bytes, r.err
}

// Gossip blocks until the peer captures AppGossip to return.
func (p *peer) Gossip(ctx context.Context) (ids.NodeID, []byte) {
	r, _ := p.gossip.PopLeft()
	return r.nodeID, r.bytes
}

func (p *peer) NodeID() ids.NodeID      { return p.id }
func (p *peer) Sender() *saetest.Sender { return p.sender }

func (p *peer) AppResponse(_ context.Context, from ids.NodeID, requestID uint32, b []byte) error {
	p.response.PushRight(peerResponse{
		nodeID:    from,
		requestID: requestID,
		bytes:     b,
	})
	return nil
}

func (p *peer) AppRequestFailed(_ context.Context, from ids.NodeID, requestID uint32, appErr *snowcommon.AppError) error {
	p.response.PushRight(peerResponse{
		nodeID:    from,
		requestID: requestID,
		err:       appErr,
	})
	return nil
}

func (p *peer) AppGossip(_ context.Context, nodeID ids.NodeID, b []byte) error {
	p.gossip.PushRight(peerGossip{
		nodeID: nodeID,
		bytes:  b,
	})
	return nil
}

func (p *peer) AppRequest(ctx context.Context, from ids.NodeID, requestID uint32, _ time.Time, _ []byte) error {
	return p.sender.SendAppError(
		ctx,
		from,
		requestID,
		p2p.ErrUnexpected.Code,
		p2p.ErrUnexpected.Message,
	)
}

func (*peer) Connected(context.Context, ids.NodeID, *version.Application) error { return nil }
func (*peer) Disconnected(context.Context, ids.NodeID) error                    { return nil }

// TestSendWarpMessage verifies that a warp message emitted by a tx is only
// available for signing AFTER the block containing the tx has executed.
func TestSendWarpMessage(t *testing.T) {
	ethWallet := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	sender := ethWallet.Addresses()[0]

	ctx, sut := newSUT(t, options.Func[sutConfig](func(c *sutConfig) {
		c.genesis.Alloc = saetest.MaxAllocFor(sender)
	}))
	capturer := newPeer(t, set.Set[ids.NodeID]{})
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
	verifyWarpMessage(ctx, t, sut, capturer, unsignedMessage.Bytes(), int32(warp.ParseErrCode))

	blockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(t, err)
	blockMessage := newUnsignedWarpMessage(t, sut, blockHashPayload.Bytes())
	verifyWarpMessage(ctx, t, sut, capturer, blockMessage.Bytes(), int32(warp.NotAcceptedErrCode))

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

	ctx, sut := newSUT(t,
		options.Func[sutConfig](func(c *sutConfig) {
			c.genesis.Alloc = saetest.MaxAllocFor(sender)
		}),
		withWarpValidators(2),
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
			signedMsg:      signWarpMessage(t, sut, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "invalid warp message",
			validPredicate: false,
			signedMsg:      fakeSignWarpMessage(t, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "valid warp block hash",
			validPredicate: true,
			signedMsg:      signWarpMessage(t, sut, blockHashMessage),
			txPayload:      blockHashTxPayload,
		},
		{
			name:           "invalid warp block hash",
			validPredicate: false,
			signedMsg:      fakeSignWarpMessage(t, blockHashMessage),
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

// signWarpMessage produces a valid aggregated BLS signature over the message,
// signed by every warp validator registered on the SUT.
func signWarpMessage(t *testing.T, sut *SUT, unsigned *avalancheWarp.UnsignedMessage) *avalancheWarp.Message {
	t.Helper()
	require.NotEmpty(t, sut.validatorKeys, "SUT has no warp validators; use withWarpValidators")

	sigs := make([]*bls.Signature, len(sut.validatorKeys))
	for i, key := range sut.validatorKeys {
		sig, err := key.Sign(unsigned.Bytes())
		require.NoError(t, err)
		sigs[i] = sig
	}
	aggregated, err := bls.AggregateSignatures(sigs)
	require.NoError(t, err)

	signers := set.NewBits()
	for i := range sut.validatorKeys {
		signers.Add(i)
	}
	warpSig := &avalancheWarp.BitSetSignature{Signers: signers.Bytes()}
	copy(warpSig.Signature[:], bls.SignatureToBytes(aggregated))

	signed, err := avalancheWarp.NewMessage(unsigned, warpSig)
	require.NoError(t, err)
	return signed
}

// fakeSignWarpMessage produces a syntactically-valid warp message with an
// invalid signature, used for negative tests.
func fakeSignWarpMessage(t *testing.T, unsigned *avalancheWarp.UnsignedMessage) *avalancheWarp.Message {
	t.Helper()
	warpSig := &avalancheWarp.BitSetSignature{
		Signers:   set.NewBits().Bytes(),
		Signature: [96]byte{1, 2, 3},
	}
	signed, err := avalancheWarp.NewMessage(unsigned, warpSig)
	require.NoError(t, err)
	return signed
}

// verifyWarpMessage sends an acp118 signature request to the SUT and asserts
// that the response (delivered to capturer) matches the expected error code.
// expected == 0 means "valid message, response is non-nil and error is nil".
func verifyWarpMessage(
	ctx context.Context,
	t *testing.T,
	sut *SUT,
	capturer *peer,
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
