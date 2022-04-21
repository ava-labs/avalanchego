// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
)

var (
	_ common.Summary  = &TestSummary{}
	_ StateSyncableVM = &TestStateSyncableVM{}

	errAccept                  = errors.New("unexpectedly called Accept")
	errGetStateSyncResult      = errors.New("unexpectedly called GetStateSyncResult")
	errParseStateSyncableBlock = errors.New("unexpectedly called ParseStateSyncableBlock")
)

type TestSummary struct {
	HeightV uint64
	IDV     ids.ID
	BytesV  []byte

	T          *testing.T
	CantAccept bool
	AcceptF    func() error
}

func (s *TestSummary) Bytes() []byte  { return s.BytesV }
func (s *TestSummary) Height() uint64 { return s.HeightV }
func (s *TestSummary) ID() ids.ID     { return s.IDV }
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

	CantGetStateSyncResult,
	CantParseStateSyncableBlock bool

	GetStateSyncResultF      func() (ids.ID, uint64, error)
	ParseStateSyncableBlockF func(blkBytes []byte) (snowman.StateSyncableBlock, error)
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

func (tss *TestStateSyncableVM) ParseStateSyncableBlock(blkBytes []byte) (snowman.StateSyncableBlock, error) {
	if tss.ParseStateSyncableBlockF != nil {
		return tss.ParseStateSyncableBlockF(blkBytes)
	}
	if tss.CantParseStateSyncableBlock && tss.T != nil {
		tss.T.Fatalf("Unexpectedly called ParseStateSyncableBlock")
	}
	return nil, errParseStateSyncableBlock
}
