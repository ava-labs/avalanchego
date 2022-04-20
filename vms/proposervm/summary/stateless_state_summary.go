// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

var _ StatelessSummaryIntf = &StatelessSummary{}

type StatelessSummaryIntf interface {
	Bytes() []byte // part of common.Summary interface
	ID() ids.ID    // part of common.Summary interface

	ProposerBlockID() ids.ID
	InnerBytes() []byte

	initialize(bytes []byte) error
}

type StatelessSummary struct {
	ProBlkID     ids.ID `serialize:"true"`
	InnerSummary []byte `serialize:"true"`

	ProposerSummaryBytes []byte
	ProposerSummaryID    ids.ID
}

func (ss *StatelessSummary) Bytes() []byte { return ss.ProposerSummaryBytes }
func (ss *StatelessSummary) ID() ids.ID    { return ss.ProposerSummaryID }

func (ss *StatelessSummary) ProposerBlockID() ids.ID { return ss.ProBlkID }
func (ss *StatelessSummary) InnerBytes() []byte      { return ss.InnerSummary }

func (ss *StatelessSummary) initialize(bytes []byte) error {
	ss.ProposerSummaryBytes = bytes
	ss.ProposerSummaryID = hashing.ComputeHash256Array(bytes)
	return nil
}
