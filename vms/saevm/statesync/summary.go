// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
	"github.com/ava-labs/libevm/common"

	_ "github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

// TODO(alarso16): This needs to be verified to be a multiple the configured
// commit interval in the non-Firewood case. Can this be safely done in this
// package, or must it be done on the VM? Does this need to match the interval
// used by coreth?
const syncCommitInterval = 4096

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
	s := new(Summary)
	if err := s.UnmarshalCanoto(summaryBytes); err != nil {
		return nil, fmt.Errorf("parsing state summary: %w", err)
	}
	return s, nil
}

func (s *Summary) Bytes() []byte {
	return s.MarshalCanoto()
}

func (s *Summary) Height() uint64 {
	return s.height
}

func (s *Summary) ID() ids.ID {
	return ids.ID(s.blockHash)
}
