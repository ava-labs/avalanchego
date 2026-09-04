// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

func TestOptionsUnexpectedBlockType(t *testing.T) {
	tests := []platform.Block{
		&platform.BanffAbortBlock{},
		&platform.BanffCommitBlock{},
		&platform.BanffStandardBlock{},
		&platform.ApricotAbortBlock{},
		&platform.ApricotCommitBlock{},
		&platform.ApricotStandardBlock{},
		&platform.ApricotAtomicBlock{},
	}

	for _, blk := range tests {
		t.Run(fmt.Sprintf("%T", blk), func(t *testing.T) {
			err := blk.Visit(&options{})
			require.ErrorIs(t, err, snowman.ErrNotOracle)
		})
	}
}
