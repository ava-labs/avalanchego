// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/evm/sync/handlers"
)

// NewSelfNetwork returns a single-node [p2p.Network] that loops requests back
// to its own handlers, and a [p2p.PeerTracker] that selects that node.
func NewSelfNetwork(t *testing.T, ctx context.Context, nodeID ids.NodeID) (*p2p.Network, *p2p.PeerTracker) {
	t.Helper()

	log := loggingtest.New(t, logging.Debug)

	sender := &enginetest.Sender{}
	net, err := p2p.NewNetwork(log, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	// Waiting in cleanup keeps require on the test goroutine, where it is safe.
	var deliveries errgroup.Group
	t.Cleanup(func() {
		require.NoError(t, deliveries.Wait(), "loopback delivery must not fail")
	})

	// Loop each send back into the same network asynchronously to avoid
	// deadlocking when the response is delivered on the sending goroutine.
	sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for range nodeIDs {
			deliveries.Go(func() error {
				return net.AppRequest(ctx, nodeID, requestID, time.Time{}, requestBytes)
			})
		}
		return nil
	}
	sender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
		deliveries.Go(func() error {
			return net.AppResponse(ctx, nodeID, requestID, responseBytes)
		})
		return nil
	}
	sender.SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, code int32, message string) error {
		deliveries.Go(func() error {
			return net.AppRequestFailed(ctx, nodeID, requestID, &common.AppError{Code: code, Message: message})
		})
		return nil
	}

	require.NoError(t, net.Connected(ctx, nodeID, version.Current))

	tracker, err := p2p.NewPeerTracker(log, "synctest_peer_tracker", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err)
	tracker.Connected(nodeID, version.Current)

	return net, tracker
}

// reserved is every [common.AppError] a sync handler can return without
// declaring it, so a per-RPC sentinel must avoid all of them.
var reserved = []*common.AppError{
	p2p.ErrUnexpected,
	p2p.ErrUnregisteredHandler,
	p2p.ErrNotValidator,
	p2p.ErrThrottled,
	common.ErrUndefined,
	common.ErrTimeout,
	handlers.ErrMalformedRequest,
	handlers.ErrMarshalResponse,
}

// RequireDistinctAppErrors asserts each sentinel is identifiable by its code,
// that the code is positive, and that it collides with neither the p2p
// framework nor the handler shell.
//
// [common.AppError.Is] compares Code and nothing else, so a shared code makes
// two sentinels the same error whatever their messages say.
func RequireDistinctAppErrors(tb testing.TB, sentinels map[string]*common.AppError) {
	tb.Helper()

	seen := make(map[int32]string, len(sentinels))
	for name, sentinel := range sentinels {
		require.ErrorIsf(tb, sentinel, &common.AppError{Code: sentinel.Code},
			"%s is not matchable by its code", name)
		require.Positivef(tb, sentinel.Code,
			"%s needs a positive code, p2p and the engine own the rest", name)

		for _, r := range reserved {
			// The shell checking its own sentinels passes them in here.
			if sentinel == r {
				continue
			}
			require.NotErrorIsf(tb, sentinel, r, "%s collides with %q", name, r.Message)
		}

		other, dup := seen[sentinel.Code]
		require.Falsef(tb, dup, "%s and %s share code %d", name, other, sentinel.Code)
		seen[sentinel.Code] = name
	}
}
