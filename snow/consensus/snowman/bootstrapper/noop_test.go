// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bootstrapper

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoop(t *testing.T) {
	require := require.New(t)

	require.Empty(Noop.GetPeers(t.Context()))

	require.NoError(Noop.RecordOpinion(t.Context(), nodeID0, nil))

	blkIDs, finalized := Noop.Result(t.Context())
	require.Empty(blkIDs)
	require.False(finalized)
}
