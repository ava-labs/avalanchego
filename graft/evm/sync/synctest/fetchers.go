// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/triedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/evm/sync/client/leafproto"
	"github.com/ava-labs/avalanchego/graft/evm/sync/types"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"

	vmsevmstate "github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
	vmssynctest "github.com/ava-labs/avalanchego/vms/evm/sync/synctest"
)

// ServeLeaves returns a fetcher reading trieDB over a loopback network. Proof
// verification is the fetcher's, so a caller only ever sees verified leaves.
func ServeLeaves(t *testing.T, ctx context.Context, trieDB *triedb.Database) types.LeafFetcher {
	t.Helper()
	log := loggingtest.New(t, logging.Debug)
	net, tracker := vmssynctest.NewSelfNetwork(t, ctx, ids.GenerateTestNodeID())
	require.NoError(t, vmsevmstate.RegisterHandler(log, net, p2p.EVMLeafRequestHandlerID, trieDB, common.HashLength))
	return leafproto.NewClient(log, vmsevmstate.NewClient(net, p2p.EVMLeafRequestHandlerID, tracker))
}

// RecordLeaves is [ServeLeaves] with every range recorded.
func RecordLeaves(t *testing.T, ctx context.Context, trieDB *triedb.Database) *RecordingFetcher {
	t.Helper()
	return NewRecordingFetcher(ServeLeaves(t, ctx, trieDB))
}

// RecordingFetcher records every range reaching inner.
type RecordingFetcher struct {
	inner types.LeafFetcher

	lock     sync.Mutex
	requests []types.LeafRange
}

func NewRecordingFetcher(inner types.LeafFetcher) *RecordingFetcher {
	return &RecordingFetcher{inner: inner}
}

func (r *RecordingFetcher) FetchLeaves(ctx context.Context, req types.LeafRange) (types.Leaves, bool, error) {
	r.lock.Lock()
	r.requests = append(r.requests, req)
	r.lock.Unlock()
	return r.inner.FetchLeaves(ctx, req)
}

// Requests returns the ranges fetched so far, in arrival order.
func (r *RecordingFetcher) Requests() []types.LeafRange {
	r.lock.Lock()
	defer r.lock.Unlock()
	return append([]types.LeafRange(nil), r.requests...)
}

// CancelAfterFetcher cancels once the at-th range arrives, ending a sync that
// would otherwise converge. A non-positive at never cancels.
type CancelAfterFetcher struct {
	inner  types.LeafFetcher
	cancel context.CancelFunc
	at     int

	seen atomic.Int32
}

func NewCancelAfterFetcher(inner types.LeafFetcher, at int, cancel context.CancelFunc) *CancelAfterFetcher {
	return &CancelAfterFetcher{inner: inner, cancel: cancel, at: at}
}

func (c *CancelAfterFetcher) FetchLeaves(ctx context.Context, req types.LeafRange) (types.Leaves, bool, error) {
	if seen := int(c.seen.Add(1)); c.at > 0 && seen >= c.at {
		c.cancel()
	}
	return c.inner.FetchLeaves(ctx, req)
}
