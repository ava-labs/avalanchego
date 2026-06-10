// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/crypto"

	_ "github.com/ava-labs/avalanchego/snow/engine/snowman/block"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

var _ adaptor.SummaryProperties = (*Summary)(nil)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// Summary is the minimal necessary information to implement a [block.StateSummary]
//
//nolint:revive // struct-tag: canoto allows unexported fields
type Summary struct {
	height    uint64      `canoto:"uint,1"`
	blockHash common.Hash `canoto:"fixed bytes,2"`

	canotoData canotoData_Summary
}

func (*SummaryHandler) ParseStateSummary(_ context.Context, summaryBytes []byte) (*Summary, error) {
	var s Summary
	if err := s.UnmarshalCanoto(summaryBytes); err != nil {
		return nil, fmt.Errorf("parsing state summary: %w", err)
	}
	return &s, nil
}

// NewSummary returns a [Summary] wrapping the block at the given height and
// hash.
func NewSummary(hash common.Hash, height uint64) *Summary {
	return &Summary{
		height:    height,
		blockHash: hash,
	}
}

func (s *Summary) Bytes() []byte {
	return s.MarshalCanoto()
}

func (s *Summary) Height() uint64 {
	return s.height
}

func (s *Summary) ID() ids.ID {
	// TODO(alarso16): should we cache this?
	return ids.ID(crypto.Keccak256Hash(s.Bytes()))
}
