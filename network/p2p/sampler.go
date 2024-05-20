package p2p

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
)

type Sampler interface {
	Sample(ctx context.Context, nodeIDs []ids.NodeID) []ids.NodeID
}
