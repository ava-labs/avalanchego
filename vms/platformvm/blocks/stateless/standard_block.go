// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ Block = &StandardBlock{}

type StandardBlock struct {
	CommonBlock `serialize:"true"`

	Txs []*txs.Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) Initialize(bytes []byte) error {
	if err := sb.CommonBlock.Initialize(bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (sb *StandardBlock) BlockTxs() []*txs.Tx { return sb.Txs }

func (sb *StandardBlock) Verify() error {
	return sb.VerifyStandardBlock(sb)
}

func (sb *StandardBlock) Accept() error {
	return sb.AcceptStandardBlock(sb)
}

func (sb *StandardBlock) Reject() error {
	return sb.RejectStandardBlock(sb)
}

func (sb *StandardBlock) Timestamp() time.Time {
	return time.Time{}
}

func NewStandardBlock(
	parentID ids.ID,
	height uint64,
	txes []*txs.Tx,
	verifier BlockVerifier,
	acceptor BlockAcceptor,
	rejector BlockRejector,
	statuser Statuser,
	timestamper Timestamper,
) (*StandardBlock, error) {
	res := &StandardBlock{
		CommonBlock: CommonBlock{
			BlockVerifier: verifier,
			BlockAcceptor: acceptor,
			BlockRejector: rejector,
			Statuser:      statuser,
			Timestamper:   timestamper,
			PrntID:        parentID,
			Hght:          height,
		},
		Txs: txes,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := Block(res)
	bytes, err := Codec.Marshal(Version, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	for _, tx := range res.Txs {
		if err := tx.Sign(txs.Codec, nil); err != nil {
			return nil, fmt.Errorf("failed to sign block: %w", err)
		}
	}

	return res, res.Initialize(bytes)
}
