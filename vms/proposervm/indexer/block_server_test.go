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
	errLastAcceptedWrappingBlkID = errors.New("unexpectedly called LastAcceptedWrappingBlkID")
	errLastAcceptedInnerBlkID    = errors.New("unexpectedly called LastAcceptedInnerBlkID")
	errGetWrappingBlk            = errors.New("unexpectedly called GetWrappingBlk")
	errGetInnerBlk               = errors.New("unexpectedly called GetInnerBlk")

	_ BlockServer = &TestBlockServer{}
)

// TestBatchedVM is a BatchedVM that is useful for testing.
type TestBlockServer struct {
	T *testing.T

	CantLastAcceptedWrappingBlkID bool
	CantLastAcceptedInnerBlkID    bool
	CantGetWrappingBlk            bool
	CantGetInnerBlk               bool

	LastAcceptedWrappingBlkIDF func() (ids.ID, error)
	LastAcceptedInnerBlkIDF    func() (ids.ID, error)
	GetWrappingBlkF            func(blkID ids.ID) (WrappingBlock, error)
	GetInnerBlkF               func(id ids.ID) (snowman.Block, error)
}

func (tsb *TestBlockServer) LastAcceptedWrappingBlkID() (ids.ID, error) {
	if tsb.LastAcceptedWrappingBlkIDF != nil {
		return tsb.LastAcceptedWrappingBlkIDF()
	}
	if tsb.CantLastAcceptedWrappingBlkID && tsb.T != nil {
		tsb.T.Fatal(errLastAcceptedWrappingBlkID)
	}
	return ids.Empty, errLastAcceptedWrappingBlkID
}

func (tsb *TestBlockServer) LastAcceptedInnerBlkID() (ids.ID, error) {
	if tsb.LastAcceptedInnerBlkIDF != nil {
		return tsb.LastAcceptedInnerBlkIDF()
	}
	if tsb.CantLastAcceptedInnerBlkID && tsb.T != nil {
		tsb.T.Fatal(errLastAcceptedInnerBlkID)
	}
	return ids.Empty, errLastAcceptedInnerBlkID
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

func (tsb *TestBlockServer) GetInnerBlk(id ids.ID) (snowman.Block, error) {
	if tsb.GetInnerBlkF != nil {
		return tsb.GetInnerBlkF(id)
	}
	if tsb.CantGetInnerBlk && tsb.T != nil {
		tsb.T.Fatal(errGetInnerBlk)
	}
	return nil, errGetInnerBlk
}
