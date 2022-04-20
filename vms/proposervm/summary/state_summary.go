// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ common.Summary = &ProposerSummary{}

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

// ProposerSummary key is summary block height and matches CoreSummaryContent key;
// However ProposerSummary ID is different from CoreSummaryContent ID
// since it hashes ProBlkID along with Core Summary Bytes for full verification.
type ProposerSummary struct {
	StatelessSummary `serialize:"true"`

	proSummaryID ids.ID
	proContent   []byte
	key          uint64
}

func (ps *ProposerSummary) Bytes() []byte { return ps.proContent }
func (ps *ProposerSummary) Key() uint64   { return ps.key }
func (ps *ProposerSummary) ID() ids.ID    { return ps.proSummaryID }
