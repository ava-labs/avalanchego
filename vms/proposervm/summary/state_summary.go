// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
)

var (
	_ ProposerSummary = &proposerSummary{}

	Codec codec.Manager

	errWrongStateSyncVersion = errors.New("wrong state sync key version")
)

const codecVersion = 0

func init() {
	lc := linearcodec.NewCustomMaxLength(math.MaxUint32)
	Codec = codec.NewManager(math.MaxInt32)
	if err := Codec.RegisterCodec(codecVersion, lc); err != nil {
		panic(err)
	}
}

type ProposerSummary interface {
	StatelessSummaryIntf
	Height() uint64 // part of block.StateSummary interface
}

func NewProposerSummary(
	statelessSummary StatelessSummaryIntf,
	height uint64,
) ProposerSummary {
	return &proposerSummary{
		StatelessSummaryIntf: statelessSummary,
		SummaryHeight:        height,
	}
}

type proposerSummary struct {
	StatelessSummaryIntf
	SummaryHeight uint64
}

func (ps *proposerSummary) Height() uint64 { return ps.SummaryHeight }
