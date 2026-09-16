// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"context"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/p2ptest"
)

// NewProofClient returns a client that routes to its own handler, scoring against 
// a tracker the test does not observe.
func NewProofClient(t *testing.T, ctx context.Context, handler p2p.Handler) *p2p.TrackingClient {
	t.Helper()

	return p2ptest.NewSelfTrackingClient(t, ctx, ids.EmptyNodeID, handler, p2ptest.NewTracker(t))
}
