// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateless

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"
)

var (
	_ StandardBlockIntf = &StandardBlock{}
	_ StandardBlockIntf = &PostForkStandardBlock{}
)

type StandardBlockIntf interface {
	CommonBlockIntf

	// StandardCommonComponents return CommonBlockIntf
	// needed to create a stateful block
	StandardCommonComponents() CommonBlockIntf

	// DecisionTxs returns list of transactions
	// contained in the block
	DecisionTxs() []*signed.Tx
}

// TODO ABENEGIA: NewStandardBlock should accept codec version
// and switch across them
func NewStandardBlock(version uint16, parentID ids.ID, height uint64, txs []*signed.Tx) (StandardBlockIntf, error) {
	res := &StandardBlock{
		CommonBlock: CommonBlock{
			PrntID: parentID,
			Hght:   height,
		},
		Txs: txs,
	}

	// We serialize this block as a Block so that it can be deserialized into a
	// Block
	blk := CommonBlockIntf(res)
	bytes, err := Codec.Marshal(PreForkVersion, &blk)
	if err != nil {
		return nil, fmt.Errorf("couldn't marshal abort block: %w", err)
	}

	for _, tx := range res.Txs {
		if err := tx.Sign(unsigned.Codec, nil); err != nil {
			return nil, fmt.Errorf("failed to sign block: %w", err)
		}
	}

	return res, res.Initialize(version, bytes)
}

type StandardBlock struct {
	CommonBlock `serialize:"true"`

	Txs []*signed.Tx `serialize:"true" json:"txs"`
}

func (sb *StandardBlock) Initialize(version uint16, bytes []byte) error {
	if err := sb.CommonBlock.Initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}
	for _, tx := range sb.Txs {
		if err := tx.Sign(unsigned.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
	}
	return nil
}

func (sb *StandardBlock) StandardCommonComponents() CommonBlockIntf {
	return &sb.CommonBlock
}

func (sb *StandardBlock) DecisionTxs() []*signed.Tx { return sb.Txs }

type PostForkStandardBlock struct {
	CommonBlock `serialize:"true"`

	TxsBytes [][]byte `serialize:"true" json:"txs"`

	Txs []*signed.Tx
}

func (psb *PostForkStandardBlock) Initialize(version uint16, bytes []byte) error {
	if err := psb.CommonBlock.Initialize(version, bytes); err != nil {
		return fmt.Errorf("failed to initialize: %w", err)
	}

	txs := make([]*signed.Tx, 0, len(psb.TxsBytes))
	for _, txBytes := range psb.TxsBytes {
		// TODO ABENEGIA: more future_proof allowing tx.Sign to accept a version
		var tx *signed.Tx
		_, err := unsigned.Codec.Unmarshal(txBytes, tx)
		if err != nil {
			return fmt.Errorf("failed unmarshalling tx in post fork block: %w", err)
		}
		if err := tx.Sign(unsigned.Codec, nil); err != nil {
			return fmt.Errorf("failed to sign block: %w", err)
		}
		txs = append(txs, tx)
	}
	psb.Txs = txs
	return nil
}

func (psb *PostForkStandardBlock) StandardCommonComponents() CommonBlockIntf {
	return &psb.CommonBlock
}

func (psb *PostForkStandardBlock) DecisionTxs() []*signed.Tx { return psb.Txs }
