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
	"github.com/ava-labs/avalanchego/vms/saevm/statesync"
)

var _ adaptor.SummaryProperties = (*summary)(nil)

//go:generate go run github.com/StephenButtolph/canoto/canoto $GOFILE

// summary wraps a [statesync.summary] with the C-Chain atomic trie root at
// the settled height.
//
//nolint:revive // struct-tag: canoto allows unexported fields
type summary struct {
	summary     statesync.Summary `canoto:"value,1"`
	settledRoot common.Hash       `canoto:"fixed bytes,2"`

	canotoData canotoData_summary
}

// ParseStateSummary unmarshals a canoto-encoded [summary].
func (*SummaryHandler) ParseStateSummary(_ context.Context, summaryBytes []byte) (*summary, error) {
	var s summary
	if err := s.UnmarshalCanoto(summaryBytes); err != nil {
		return nil, fmt.Errorf("parsing state summary: %w", err)
	}
	return &s, nil
}

func (s *summary) Bytes() []byte {
	return s.MarshalCanoto()
}

func (s *summary) Height() uint64 {
	return s.summary.Height()
}

func (s *summary) ID() ids.ID {
	// TODO(alarso16): should we cache this?
	return ids.ID(crypto.Keccak256Hash(s.Bytes()))
}
