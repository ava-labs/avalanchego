// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ StateSyncableVM = &TestStateSyncableVM{}

	errGetLastSummaryBlockID = errors.New("unexpectedly called GetLastSummaryBlockID")
	errSetLastSummaryBlock   = errors.New("unexpectedly called SetLastSummaryBlock")
)

type TestStateSyncableVM struct {
	common.TestStateSyncableVM

	CantGetLastSummaryBlockID, CantSetLastSummaryBlock bool

	GetLastSummaryBlockIDF func() (ids.ID, error)
	SetLastSummaryBlockF   func([]byte) error
}

func (tss *TestStateSyncableVM) GetLastSummaryBlockID() (ids.ID, error) {
	if tss.GetLastSummaryBlockIDF != nil {
		return tss.GetLastSummaryBlockIDF()
	}
	if tss.CantGetLastSummaryBlockID && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called GetLastSummaryBlockID")
	}
	return ids.Empty, errGetLastSummaryBlockID
}

func (tss *TestStateSyncableVM) SetLastSummaryBlock(blkBytes []byte) error {
	if tss.SetLastSummaryBlockF != nil {
		return tss.SetLastSummaryBlockF(blkBytes)
	}
	if tss.CantSetLastSummaryBlock && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called SetLastSummaryBlock")
	}
	return errSetLastSummaryBlock
}
