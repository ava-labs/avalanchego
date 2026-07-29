// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

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

// NewSelfNetwork returns a single-node [p2p.Network] that loops every request
// back to its own handlers, and a [p2p.PeerTracker] that selects that node.
// Register handlers on the returned network and build a client against it to
// drive a full request/response round trip in-process.
func NewSelfNetwork(t *testing.T, ctx context.Context, nodeID ids.NodeID) (*p2p.Network, *p2p.PeerTracker) {
	t.Helper()

	sender := &enginetest.Sender{}
	net, err := p2p.NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
	require.NoError(t, err)

	log := loggingtest.New(t, logging.Debug)

	// Joining the delivery goroutines keeps them from outliving the test,
	// so their logs can never reach a completed [testing.T].
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	deliver := func(name string, fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fn(); err != nil {
				log.Debug(name, zap.Error(err))
			}
		}()
	}

	// Loop each send back into the same network asynchronously to avoid
	// deadlocking when the response is delivered on the sending goroutine.
	sender.SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for range nodeIDs {
			deliver("AppRequest", func() error {
				return net.AppRequest(ctx, nodeID, requestID, time.Time{}, requestBytes)
			})
		}
		return nil
	}
	sender.SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
		deliver("AppResponse", func() error {
			return net.AppResponse(ctx, nodeID, requestID, responseBytes)
		})
		return nil
	}
	sender.SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, code int32, message string) error {
		deliver("AppRequestFailed", func() error {
			return net.AppRequestFailed(ctx, nodeID, requestID, &common.AppError{Code: code, Message: message})
		})
		return nil
	}

	require.NoError(t, net.Connected(ctx, nodeID, nil))

	tracker, err := p2p.NewPeerTracker(logging.NoLog{}, "synctest_peer_tracker", prometheus.NewRegistry(), nil, nil)
	require.NoError(t, err)
	tracker.Connected(nodeID, &version.Application{Major: 99})

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
