// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.Summary  = &TestSummary{}
	_ StateSyncableVM = &TestStateSyncableVM{}

	errStateSyncGetResult           = errors.New("unexpectedly called StateSyncGetResult")
	errStateSyncSetLastSummaryBlock = errors.New("unexpectedly called StateSyncSetLastSummaryBlock")
)

type TestSummary struct {
	SummaryKey   uint64
	SummaryID    ids.ID
	ContentBytes []byte
}

func (s *TestSummary) Bytes() []byte { return s.ContentBytes }
func (s *TestSummary) Key() uint64   { return s.SummaryKey }
func (s *TestSummary) ID() ids.ID    { return s.SummaryID }

type TestStateSyncableVM struct {
	common.TestStateSyncableVM

	CantStateSyncGetResult, CantStateSyncSetLastSummaryBlock bool

	StateSyncGetResultF             func() (ids.ID, uint64, error)
	StateSyncSetLastSummaryBlockIDF func(blkID ids.ID) error
}

func (tss *TestStateSyncableVM) StateSyncGetResult() (ids.ID, uint64, error) {
	if tss.StateSyncGetResultF != nil {
		return tss.StateSyncGetResultF()
	}
	if tss.CantStateSyncGetResult && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncGetResult")
	}
	return ids.Empty, 0, errStateSyncGetResult
}

func (tss *TestStateSyncableVM) StateSyncSetLastSummaryBlockID(blkID ids.ID) error {
	if tss.StateSyncSetLastSummaryBlockIDF != nil {
		return tss.StateSyncSetLastSummaryBlockIDF(blkID)
	}
	if tss.CantStateSyncSetLastSummaryBlock && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called StateSyncSetLastSummaryBlock")
	}
	return errStateSyncSetLastSummaryBlock
}
