// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
	"github.com/ava-labs/avalanchego/vms/saevm/txgossip/txgossiptest"

	saerpc "github.com/ava-labs/avalanchego/vms/saevm/sae/rpc"
)

// rawVM is the native block API common to the SAE and C-Chain VMs. Both expose
// it directly, the latter by embedding the former.
type rawVM interface {
	SetPreference(context.Context, ids.ID, *block.Context) error
	BuildBlock(context.Context, *block.Context) (*blocks.Block, error)
	VerifyBlock(context.Context, *block.Context, *blocks.Block) error
	AcceptBlock(context.Context, *blocks.Block) error
	LastAccepted(context.Context) (ids.ID, error)
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
	WaitForEvent(context.Context) (common.Message, error)
	GethRPCBackends() saerpc.GethBackends
}

// SUT is the VM-agnostic system under test. The VM type parameter lets each
// package reach its own internals through [SUT.RawVM] while sharing the
// consensus helpers below.
type SUT[VM rawVM] struct {
	RawVM     VM
	EthClient *ethclient.Client
	AppSender *saetest.Sender
	Logger    *loggingtest.Logger
}

// Sender returns the mock app-message sender, satisfying [saetest.Peer].
func (s *SUT[VM]) Sender() *saetest.Sender { return s.AppSender }

// Context returns a [testing.TB]-scoped context that is cancelled when the
// logger records a log at [logging.Error] or above.
//
//nolint:thelper // Not a helper
func (s *SUT[VM]) Context(tb testing.TB) context.Context {
	return s.Logger.CancelOnError(tb.Context())
}

// LastAcceptedID returns the last-accepted block's ID.
func (s *SUT[VM]) LastAcceptedID(tb testing.TB) ids.ID {
	tb.Helper()
	id, err := s.RawVM.LastAccepted(s.Context(tb))
	require.NoError(tb, err, "LastAccepted()")
	return id
}

// LastAcceptedBlock returns the last-accepted block.
func (s *SUT[VM]) LastAcceptedBlock(tb testing.TB) *blocks.Block {
	tb.Helper()
	ctx := s.Context(tb)
	id, err := s.RawVM.LastAccepted(ctx)
	require.NoError(tb, err, "LastAccepted()")
	b, err := s.RawVM.GetBlock(ctx, id)
	require.NoError(tb, err, "GetBlock(lastAcceptedID)")
	return b
}

// WaitForPendingTxs blocks until the VM reports pending transactions.
func (s *SUT[VM]) WaitForPendingTxs(tb testing.TB) {
	tb.Helper()
	msg, err := s.RawVM.WaitForEvent(s.Context(tb))
	require.NoErrorf(tb, err, "%T.WaitForEvent()", s.RawVM)
	require.Equalf(tb, common.PendingTxs, msg, "%T.WaitForEvent() message", s.RawVM)
}

// MustSendTx submits each tx via the eth client, failing the test on error.
func (s *SUT[VM]) MustSendTx(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	ctx := s.Context(tb)
	for _, tx := range txs {
		require.NoErrorf(tb, s.EthClient.SendTransaction(ctx, tx), "%T.SendTransaction([%#x])", s.EthClient, tx.Hash())
	}
}

// WaitUntilTxsPending blocks until all txs are pending in the source the block
// builder draws from.
func (s *SUT[VM]) WaitUntilTxsPending(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	txgossiptest.WaitUntilPending(tb, s.Context(tb), s.RawVM.GethRPCBackends(), txs...)
}

// SendTxsAndWaitUntilPending submits each tx and waits until all are pending in
// the source the block builder draws from.
func (s *SUT[VM]) SendTxsAndWaitUntilPending(tb testing.TB, txs ...*types.Transaction) {
	tb.Helper()
	s.MustSendTx(tb, txs...)
	s.WaitUntilTxsPending(tb, txs...)
}

// BuildVerify builds a block on top of preferenceID and verifies it, without a
// block context.
func (s *SUT[VM]) BuildVerify(tb testing.TB, preferenceID ids.ID) *blocks.Block {
	tb.Helper()
	ctx := s.Context(tb)
	require.NoErrorf(tb, s.RawVM.SetPreference(ctx, preferenceID, nil), "%T.SetPreference()", s.RawVM)
	b, err := s.RawVM.BuildBlock(ctx, nil)
	require.NoErrorf(tb, err, "%T.BuildBlock()", s.RawVM)
	require.NoErrorf(tb, s.RawVM.VerifyBlock(ctx, nil, b), "%T.VerifyBlock()", s.RawVM)
	return b
}

// RunConsensusLoopOnPreference builds, verifies, and accepts a block on top of
// preferenceID. It does NOT wait for execution.
func (s *SUT[VM]) RunConsensusLoopOnPreference(tb testing.TB, preferenceID ids.ID) *blocks.Block {
	tb.Helper()
	b := s.BuildVerify(tb, preferenceID)
	require.NoErrorf(tb, s.RawVM.AcceptBlock(s.Context(tb), b), "%T.AcceptBlock()", s.RawVM)
	return b
}

// RunConsensusLoop is [SUT.RunConsensusLoopOnPreference] on top of the
// last-accepted block.
func (s *SUT[VM]) RunConsensusLoop(tb testing.TB) *blocks.Block {
	tb.Helper()
	return s.RunConsensusLoopOnPreference(tb, s.LastAcceptedID(tb))
}

// Dial dials url and returns the RPC client and an [ethclient.Client] sharing
// its transport, which is closed during cleanup.
func Dial(tb testing.TB, url string) (*rpc.Client, *ethclient.Client) {
	tb.Helper()
	rpcClient, err := rpc.Dial(url)
	require.NoErrorf(tb, err, "rpc.Dial(%q)", url)
	tb.Cleanup(rpcClient.Close)
	return rpcClient, ethclient.NewClient(rpcClient)
}
