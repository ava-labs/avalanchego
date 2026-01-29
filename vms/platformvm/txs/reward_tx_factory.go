// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
)

func NewRewardTxForStaker(ctx *snow.Context, stakerTx *Tx, timestamp time.Time) (*Tx, error) {
	txID := stakerTx.ID()
	if _, ok := stakerTx.Unsigned.(ContinuousStaker); ok {
		return NewRewardContinuousValidatorTx(ctx, txID, uint64(timestamp.Unix()))
	}

	return NewRewardValidatorTx(ctx, txID)
}

func NewRewardValidatorTx(ctx *snow.Context, txID ids.ID) (*Tx, error) {
	utx := &RewardValidatorTx{TxID: txID}
	tx, err := NewSigned(utx, Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(ctx)
}

func NewRewardContinuousValidatorTx(ctx *snow.Context, txID ids.ID, timestamp uint64) (*Tx, error) {
	utx := &RewardContinuousValidatorTx{TxID: txID, Timestamp: timestamp}
	tx, err := NewSigned(utx, Codec, nil)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(ctx)
}
