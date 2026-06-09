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
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
)

// rawVM is the block-lookup surface shared by *sae.VM and, via embedding,
// *cchain.VM.
type rawVM interface {
	LastAccepted(context.Context) (ids.ID, error)
	GetBlock(context.Context, ids.ID) (*blocks.Block, error)
}

// SUT is the VM-agnostic system under test. VM is the concrete raw VM type, so
// each package reaches its own internals via [SUT.RawVM] while sharing the
// helpers below.
type SUT[VM rawVM] struct {
	RawVM  VM
	Logger *loggingtest.Logger
}

// Context returns a [context.Context], derived from the [testing.TB], that is
// cancelled if the SUT's logger receives a log at [logging.Error] or higher.
//
//nolint:thelper // Not a helper
func (s *SUT[VM]) Context(tb testing.TB) context.Context {
	return s.Logger.CancelOnError(tb.Context())
}

// LastAcceptedID returns the ID of the last-accepted block.
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

// DialWS dials the websocket RPC endpoint at wsURL, returning the raw client
// alongside an [ethclient.Client] built on top of it. The shared transport is
// closed during [testing.TB] cleanup.
func DialWS(tb testing.TB, wsURL string) (*rpc.Client, *ethclient.Client) {
	tb.Helper()
	rpcClient, err := rpc.Dial(wsURL)
	require.NoErrorf(tb, err, "rpc.Dial(%q)", wsURL)
	tb.Cleanup(rpcClient.Close)
	return rpcClient, ethclient.NewClient(rpcClient)
}
