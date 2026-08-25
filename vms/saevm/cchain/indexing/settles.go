// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexing

import (
	"fmt"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain"
	"github.com/ava-labs/libevm/core/types"
)

// Settles returns the slice of contiguous block heights that were newly settled
// by acceptance of the specified block. The [types.Header.Bloom] of the block
// pertains to transactions included in the returned heights.
func Settles(block, parent *types.Header) ([]uint64, error) {
	return withTempLibEVMExtras(func() ([]uint64, error) {
		if block.ParentHash != parent.Hash() {
			return nil, fmt.Errorf("block %#x is not parent of %#x; expecting %#x", parent.Hash(), block.Hash(), block.ParentHash)
		}

		switch s, p := cchain.SettledBy(block), cchain.SettledBy(parent); {
		case s.IsSynchronous():
			// Synchronous blocks are, by definition, self-settling.
			return []uint64{block.Number.Uint64()}, nil

		case p.IsSynchronous():
			// A self-settling parent leaves no intermediate blocks to settle.
			return []uint64{}, nil

		default:
			heights := make([]uint64, s.Height-p.Height)
			for i := range heights {
				heights[i] = p.Height + uint64(i) + 1
			}
			return heights, nil
		}
	})
}

func withTempLibEVMExtras[T any](fn func() (T, error)) (T, error) {
	var x T
	err := evm.WithTempRegisteredLibEVMExtras(func() error {
		var err error
		x, err = fn()
		return err
	})
	if err != nil {
		return utils.Zero[T](), err
	}
	return x, nil
}
