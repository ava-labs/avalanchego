// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowmantest

import (
	"github.com/stretchr/testify/require"

	"github.com/MetalBlockchain/metalgo/snow/snowtest"
)

func RequireStatusIs(require *require.Assertions, status snowtest.Status, blks ...*Block) {
	for i, blk := range blks {
		require.Equal(status, blk.Status, i)
	}
}
