// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import "github.com/ava-labs/avalanchego/ids"

var _ StateSummary = (*stateSummary)(nil)

type StateSummary interface {
	ID() ids.ID
	ForkHeight() uint64
	BlockBytes() []byte
	InnerSummaryBytes() []byte
	Bytes() []byte
}

type stateSummary struct {
	Height uint64 `serialize:"true"`
	// TODO: Rather than storing the full block here - we should only store
	//       proposervm information. We would then modify the StateSummary
	//       interface to expose the required information to generate the full
	//       block.
	Block        []byte `serialize:"true"`
	InnerSummary []byte `serialize:"true"`

	id    ids.ID
	bytes []byte
}

func (s *stateSummary) ID() ids.ID {
	return s.id
}

func (s *stateSummary) ForkHeight() uint64 {
	return s.Height
}

func (s *stateSummary) BlockBytes() []byte {
	return s.Block
}

func (s *stateSummary) InnerSummaryBytes() []byte {
	return s.InnerSummary
}

func (s *stateSummary) Bytes() []byte {
	return s.bytes
}
