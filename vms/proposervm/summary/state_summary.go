// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ ProposerSummary = &proposerSummary{}

	cdc codec.Manager

	errWrongStateSyncVersion = errors.New("wrong state sync key version")
)

const codecVersion = 0

func init() {
	lc := linearcodec.NewCustomMaxLength(math.MaxUint32)
	cdc = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&StatelessSummary{}),

		cdc.RegisterCodec(codecVersion, lc),
	)
	if errs.Errored() {
		panic(errs.Err)
	}
}

type ProposerSummary interface {
	StatelessSummaryIntf
	Height() uint64 // part of block.Summary interface
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
