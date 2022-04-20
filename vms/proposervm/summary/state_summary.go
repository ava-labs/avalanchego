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
	_ ProposerSummaryIntf = &ProposerSummary{}

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

type ProposerSummaryIntf interface {
	StatelessSummaryIntf
	Key() uint64
}

type ProposerSummary struct {
	StatelessSummary `serialize:"true"`

	SummaryKey uint64
}

func (ps *ProposerSummary) Key() uint64 { return ps.SummaryKey }
