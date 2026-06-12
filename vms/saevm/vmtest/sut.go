// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vmtest

import (
	"context"
	"testing"

	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	snowcommon "github.com/ava-labs/avalanchego/snow/engine/common"
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
	WaitForEvent(context.Context) (snowcommon.Message, error)
}

// SUT is the VM-agnostic system under test. The VM type parameter lets each
// package reach its own internals through [SUT.RawVM] while sharing the
// consensus helpers below.
type SUT[VM rawVM] struct {
	RawVM  VM
	Logger *loggingtest.Logger
}

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
	require.Equalf(tb, snowcommon.PendingTxs, msg, "%T.WaitForEvent() message", s.RawVM)
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

// DialWS dials wsURL and returns the RPC client and an [ethclient.Client]
// sharing its transport, which is closed during cleanup.
func DialWS(tb testing.TB, wsURL string) (*rpc.Client, *ethclient.Client) {
	tb.Helper()
	rpcClient, err := rpc.Dial(wsURL)
	require.NoErrorf(tb, err, "rpc.Dial(%q)", wsURL)
	tb.Cleanup(rpcClient.Close)
	return rpcClient, ethclient.NewClient(rpcClient)
}
