// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ ProposerContent = &summaryContent{}

	stateSyncCodec codec.Manager

	errWrongStateSyncVersion = errors.New("wrong state sync key version")
)

const StateSummaryVersion = 0

func init() {
	lc := linearcodec.NewCustomMaxLength(math.MaxUint32)
	stateSyncCodec = codec.NewManager(math.MaxInt32)

	errs := wrappers.Errs{}
	errs.Add(
		lc.RegisterType(&summaryContent{}),
		stateSyncCodec.RegisterCodec(StateSummaryVersion, lc),
	)
	if err := errs.Err; err != nil {
		panic(err)
	}
}

// ProposerContent adds to its Core Summary Content the proposer block ID
// associated with the summary. This allows retrieving the full block associated
// with state summary once state syncing is done.
type ProposerContent interface {
	common.Summary

	ProposerBlockID() ids.ID
	CoreSummaryBytes() []byte
}

// summaryContent key is summary block height and matches CoreSummaryContent key;
// However summaryContent ID is different from CoreSummaryContent ID
// since it hashes ProBlkID along with Core Summary Bytes for full verification.
type summaryContent struct {
	ProBlkID    ids.ID `serialize:"true"`
	CoreContent []byte `serialize:"true"`

	proSummaryID ids.ID
	proContent   []byte
	key          uint64
}

func (ps *summaryContent) Bytes() []byte { return ps.proContent }
func (ps *summaryContent) Key() uint64   { return ps.key }
func (ps *summaryContent) ID() ids.ID    { return ps.proSummaryID }

func (ps *summaryContent) ProposerBlockID() ids.ID  { return ps.ProBlkID }
func (ps *summaryContent) CoreSummaryBytes() []byte { return ps.CoreContent }

func New(proBlkID ids.ID, coreSummary common.Summary) (ProposerContent, error) {
	res := &summaryContent{
		ProBlkID:    proBlkID,
		CoreContent: coreSummary.Bytes(),

		key: coreSummary.Key(), // note: this is not serialized
	}

	proContent, err := stateSyncCodec.Marshal(StateSummaryVersion, res)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal proposerVMKey due to: %w", err)
	}
	res.proContent = proContent

	proSummaryID, err := ids.ToID(hashing.ComputeHash256(proContent))
	if err != nil {
		return nil, fmt.Errorf("cannot compute summary ID: %w", err)
	}
	res.proSummaryID = proSummaryID
	return res, nil
}

func Parse(summaryBytes []byte) (ProposerContent, error) {
	proContent := summaryContent{}
	ver, err := stateSyncCodec.Unmarshal(summaryBytes, &proContent)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal ProposerSummaryContent due to: %w", err)
	}
	if ver != StateSummaryVersion {
		return nil, errWrongStateSyncVersion
	}

	return &proContent, nil
}
