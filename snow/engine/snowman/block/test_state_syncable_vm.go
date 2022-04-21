// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.Summary  = &TestSummary{}
	_ StateSyncableVM = &TestStateSyncableVM{}

	errAccept                   = errors.New("unexpectedly called Accept")
	errGetStateSyncResult       = errors.New("unexpectedly called GetStateSyncResult")
	errSetLastStateSummaryBlock = errors.New("unexpectedly called SetLastStateSummaryBlock")
)

type TestSummary struct {
	SummaryHeight uint64
	SummaryID     ids.ID
	ContentBytes  []byte

	T          *testing.T
	CantAccept bool
	AcceptF    func() error
}

func (s *TestSummary) Bytes() []byte  { return s.ContentBytes }
func (s *TestSummary) Height() uint64 { return s.SummaryHeight }
func (s *TestSummary) ID() ids.ID     { return s.SummaryID }
func (s *TestSummary) Accept() error {
	if s.AcceptF != nil {
		return s.AcceptF()
	}
	if s.CantAccept && s.T != nil {
		s.T.Fatalf("Unexpectedly called Accept")
	}
	return errAccept
}

type TestStateSyncableVM struct {
	common.TestStateSyncableVM

	CantGetStateSyncResult, CantSetLastStateSummaryBlock bool

	GetStateSyncResultF       func() (ids.ID, uint64, error)
	SetLastStateSummaryBlockF func(blkBytes []byte) error
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

func (tss *TestStateSyncableVM) SetLastStateSummaryBlock(blkBytes []byte) error {
	if tss.SetLastStateSummaryBlockF != nil {
		return tss.SetLastStateSummaryBlockF(blkBytes)
	}
	if tss.CantSetLastStateSummaryBlock && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called SetLastStateSummaryBlock")
	}
	return errSetLastStateSummaryBlock
}
