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

	errGetStateSyncResult  = errors.New("unexpectedly called GetStateSyncResult")
	errSetLastSummaryBlock = errors.New("unexpectedly called SetLastSummaryBlock")
)

type TestStateSyncableVM struct {
	common.TestStateSyncableVM

	CantGetStateSyncResult, CantSetLastSummaryBlock bool

	GetStateSyncResultF  func() (ids.ID, uint64, error)
	SetLastSummaryBlockF func([]byte) error
}

func (tss *TestStateSyncableVM) GetStateSyncResult() (ids.ID, uint64, error) {
	if tss.GetStateSyncResultF != nil {
		return tss.GetStateSyncResultF()
	}
	if tss.CantGetStateSyncResult && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called GetStateSyncResult")
	}
	return ids.Empty, 0, errGetStateSyncResult
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
