// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

// NodeSampler samples nodes in network
type NodeSampler interface {
	// Sample returns at most [limit] nodes. This may return fewer nodes if
	// fewer than [limit] are available.
	Sample(ctx context.Context, limit int) []ids.NodeID
}
