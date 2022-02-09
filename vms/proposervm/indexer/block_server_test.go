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

	CantGetFullPostForkBlock bool
	CantCommit               bool

	GetFullPostForkBlockF func(blkID ids.ID) (snowman.Block, error)
	CommitF               func() error
}

func (tsb *TestBlockServer) GetFullPostForkBlock(blkID ids.ID) (snowman.Block, error) {
	if tsb.GetFullPostForkBlockF != nil {
		return tsb.GetFullPostForkBlockF(blkID)
	}
	if tsb.CantGetFullPostForkBlock && tsb.T != nil {
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
