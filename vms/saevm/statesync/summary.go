// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

var _ adaptor.SummaryProperties = (*Summary)(nil)

type Summary struct {
	block *types.Block
}

// Bytes returns an encoding of the state summary
//
// TODO: Is there a better way to encode this?
func (s *Summary) Bytes() []byte {
	b, err := rlp.EncodeToBytes(s.block)
	if err != nil {
		panic("unexpected decode err" + err.Error())
	}

	return b
}

// Height return the block noted in the state summary.
func (s *Summary) Height() uint64 {
	return s.block.NumberU64()
}

func (s *Summary) ID() ids.ID {
	return ids.ID(s.block.Hash())
}

// ParseStateSummary implements [block.StateSyncableVM].
// TODO(alarso16): find a better way to encode this.
func (vm *VM[_]) ParseStateSummary(ctx context.Context, summaryBytes []byte) (*Summary, error) {
	var block types.Block
	if err := rlp.DecodeBytes(summaryBytes[:len(summaryBytes)], &block); err != nil {
		return nil, fmt.Errorf("decode block: %w", err)
	}
	return &Summary{block: &block}, nil
}
