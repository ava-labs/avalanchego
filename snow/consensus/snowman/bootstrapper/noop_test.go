// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestNoop(t *testing.T) {
	var (
		require = require.New(t)
		ctx     = context.Background()
		nodeID  = ids.GenerateTestNodeID()
	)

	require.Empty(Noop.GetPeers(ctx))

	require.NoError(Noop.RecordOpinion(ctx, nodeID))

	blkIDs, finalized := Noop.Result(ctx)
	require.Empty(blkIDs)
	require.False(finalized)
}
