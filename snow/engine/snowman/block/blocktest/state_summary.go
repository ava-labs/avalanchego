// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blocktest

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

var (
	_ block.StateSummary = (*StateSummary)(nil)

	errAccept = errors.New("unexpectedly called Accept")
)

type StateSummary struct {
	IDV     ids.ID
	HeightV uint64
	BytesV  []byte

	T          *testing.T
	CantAccept bool
	AcceptF    func(context.Context) (block.StateSyncMode, error)
}

func (s *StateSummary) ID() ids.ID {
	return s.IDV
}

func (s *StateSummary) Height() uint64 {
	return s.HeightV
}

func (s *StateSummary) Bytes() []byte {
	return s.BytesV
}

func (s *StateSummary) Accept(ctx context.Context) (block.StateSyncMode, error) {
	if s.AcceptF != nil {
		return s.AcceptF(ctx)
	}
	if s.CantAccept && s.T != nil {
		require.FailNow(s.T, errAccept.Error())
	}
	return block.StateSyncSkipped, errAccept
}
