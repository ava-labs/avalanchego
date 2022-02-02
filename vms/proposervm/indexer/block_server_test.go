// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package indexer

import (
	"errors"
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	errGetWrappingBlk = errors.New("unexpectedly called GetWrappingBlk")
	errCommit         = errors.New("unexpectedly called Commit")

	_ BlockServer = &TestBlockServer{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestBlockServer struct {
	T *testing.T

	CantGetWrappingBlk bool
	CantCommit         bool

	GetWrappingBlkF func(blkID ids.ID) (WrappingBlock, error)
	GetInnerBlkF    func(id ids.ID) (snowman.Block, error)
	CommitF         func() error
}

func (tsb *TestBlockServer) GetWrappingBlk(blkID ids.ID) (WrappingBlock, error) {
	if tsb.GetWrappingBlkF != nil {
		return tsb.GetWrappingBlkF(blkID)
	}
	if tsb.CantGetWrappingBlk && tsb.T != nil {
		tsb.T.Fatal(errGetWrappingBlk)
	}
	return nil, errGetWrappingBlk
}

func (tsb *TestBlockServer) Commit() error {
	if tsb.CommitF != nil {
		return tsb.CommitF()
	}
	if tsb.CantCommit && tsb.T != nil {
		tsb.T.Fatal(errCommit)
	}
	return errCommit
}
