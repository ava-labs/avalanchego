// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var (
	_ UnsignedTx = (*RewardContinuousValidatorTx)(nil)

	errMissingTxID      = errors.New("missing tx id")
	errMissingTimestamp = errors.New("missing timestamp")
)

// RewardContinuousValidatorTx is a transaction that represents a proposal to
// reward/remove a continuous validator that is currently validating from the validator set.
type RewardContinuousValidatorTx struct {
	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`

	// End time of the validator.
	Timestamp uint64 `serialize:"true" json:"timestamp"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *RewardContinuousValidatorTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (*RewardContinuousValidatorTx) InitCtx(*snow.Context) {}

func (tx *RewardContinuousValidatorTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (*RewardContinuousValidatorTx) InputIDs() set.Set[ids.ID] {
	return nil
}

func (*RewardContinuousValidatorTx) Outputs() []*avax.TransferableOutput {
	return nil
}

func (tx *RewardContinuousValidatorTx) SyntacticVerify(*snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.TxID == ids.Empty:
		return errMissingTxID
	case tx.Timestamp == 0:
		return errMissingTimestamp
	}

	return nil
}

func (tx *RewardContinuousValidatorTx) Visit(visitor Visitor) error {
	return visitor.RewardContinuousValidatorTx(tx)
}
