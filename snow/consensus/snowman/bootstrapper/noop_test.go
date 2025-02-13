// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoop(t *testing.T) {
	require := require.New(t)

	require.Empty(Noop.GetPeers(context.Background()))

	require.NoError(Noop.RecordOpinion(context.Background(), nodeID0, nil))

	blkIDs, finalized := Noop.Result(context.Background())
	require.Empty(blkIDs)
	require.False(finalized)
}
