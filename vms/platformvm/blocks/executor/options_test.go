// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
)

func TestOptionsUnexpectedBlockType(t *testing.T) {
	tests := []blocks.Block{
		&blocks.BanffAbortBlock{},
		&blocks.BanffCommitBlock{},
		&blocks.BanffStandardBlock{},
		&blocks.ApricotAbortBlock{},
		&blocks.ApricotCommitBlock{},
		&blocks.ApricotStandardBlock{},
		&blocks.ApricotAtomicBlock{},
	}

	for _, blk := range tests {
		t.Run(fmt.Sprintf("%T", blk), func(t *testing.T) {
			err := blk.Visit(&options{})
			require.ErrorIs(t, err, snowman.ErrNotOracle)
		})
	}
}
