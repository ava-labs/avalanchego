// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package summary

import "github.com/ava-labs/avalanchego/ids"

var _ StatelessSummaryIntf = &StatelessSummary{}

type StatelessSummaryIntf interface {
	ProposerBlockID() ids.ID
	InnerSummaryBytes() []byte
}

type StatelessSummary struct {
	ProBlkID     ids.ID `serialize:"true"`
	InnerSummary []byte `serialize:"true"`
}

func (ss StatelessSummary) ProposerBlockID() ids.ID   { return ss.ProBlkID }
func (ss StatelessSummary) InnerSummaryBytes() []byte { return ss.InnerSummary }
