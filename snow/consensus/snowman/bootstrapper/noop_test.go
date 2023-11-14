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

	require.Empty(Noop.GetAcceptedFrontiersToSend(ctx))

	Noop.RecordAcceptedFrontier(ctx, nodeID)

	blkIDs, finalized := Noop.GetAcceptedFrontier(ctx)
	require.Empty(blkIDs)
	require.False(finalized)

	require.Empty(Noop.GetAcceptedToSend(ctx))

	require.NoError(Noop.RecordAccepted(ctx, nodeID, nil))

	blkIDs, finalized = Noop.GetAccepted(ctx)
	require.Empty(blkIDs)
	require.False(finalized)
}
