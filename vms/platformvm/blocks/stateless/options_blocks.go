// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var (
	_ Block = &AbortBlock{}
	_ Block = &CommitBlock{}
)

type AbortBlock struct {
	BlockVerifier // TODO set this field
	BlockAcceptor // TODO set this field
	BlockRejector // TODO set this field
	Statuser      // TODO set
	CommonBlock   `serialize:"true"`
}

func (ab *AbortBlock) BlockTxs() []*txs.Tx { return nil }

func (ab *AbortBlock) Verify() error {
	return ab.VerifyAbortBlock(ab)
}

func (ab *AbortBlock) Accept() error {
	return ab.AcceptAbortBlock(ab)
}

func (ab *AbortBlock) Reject() error {
	return ab.RejectAbortBlock(ab)
}

func NewAbortBlock(
	parentID ids.ID,
	height uint64,
	verifier BlockVerifier,
	acceptor BlockAcceptor,
	rejector BlockRejector,
	statuser Statuser,
	timestamper Timestamper,
) (*AbortBlock, error) {
	res := &AbortBlock{
		CommonBlock: CommonBlock{
			BlockVerifier: verifier,
			BlockAcceptor: acceptor,
			BlockRejector: rejector,
			Statuser:      statuser,
			Timestamper:   timestamper,
			PrntID:        parentID,
			Hght:          height,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(bytes)
}

type CommitBlock struct {
	CommonBlock `serialize:"true"`
}

func (cb *CommitBlock) BlockTxs() []*txs.Tx { return nil }

func (cb *CommitBlock) Verify() error {
	return cb.VerifyCommitBlock(cb)
}

func (cb *CommitBlock) Accept() error {
	return cb.AcceptCommitBlock(cb)
}

func (cb *CommitBlock) Reject() error {
	return cb.RejectCommitBlock(cb)
}

func NewCommitBlock(
	parentID ids.ID,
	height uint64,
	verifier BlockVerifier,
	acceptor BlockAcceptor,
	rejector BlockRejector,
	statuser Statuser,
	timestamper Timestamper,
) (*CommitBlock, error) {
	res := &CommitBlock{
		CommonBlock: CommonBlock{
			BlockVerifier: verifier,
			BlockAcceptor: acceptor,
			BlockRejector: rejector,
			Statuser:      statuser,
			Timestamper:   timestamper,
			PrntID:        parentID,
			Hght:          height,
		},
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	return res, res.Initialize(bytes)
}
