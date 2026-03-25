// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

var _ UnsignedTx = (*RewardAutoRenewedValidatorTx)(nil)

// RewardAutoRenewedValidatorTx is a transaction that represents a proposal to
// reward/remove an auto-renewed validator that is currently validating from the validator set.
type RewardAutoRenewedValidatorTx struct {
	// ID of the tx that created the validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`

	// End time of the validation cycle
	Timestamp uint64 `serialize:"true" json:"timestamp"`

	unsignedBytes []byte // Unsigned byte representation of this data
}

func (tx *RewardAutoRenewedValidatorTx) StakerTxID() ids.ID {
	return tx.TxID
}

func (tx *RewardAutoRenewedValidatorTx) SetBytes(unsignedBytes []byte) {
	tx.unsignedBytes = unsignedBytes
}

func (*RewardAutoRenewedValidatorTx) InitCtx(*snow.Context) {}

func (tx *RewardAutoRenewedValidatorTx) Bytes() []byte {
	return tx.unsignedBytes
}

func (*RewardAutoRenewedValidatorTx) InputIDs() set.Set[ids.ID] {
	return nil
}

func (*RewardAutoRenewedValidatorTx) Outputs() []*avax.TransferableOutput {
	return nil
}

func (*RewardAutoRenewedValidatorTx) SyntacticVerify(*snow.Context) error {
	return nil
}

func (tx *RewardAutoRenewedValidatorTx) Visit(visitor Visitor) error {
	return visitor.RewardAutoRenewedValidatorTx(tx)
}
