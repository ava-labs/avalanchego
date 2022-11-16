// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import "github.com/ava-labs/avalanchego/vms/components/avax"

var _ UnsignedTx = (*CaminoRewardValidatorTx)(nil)

// CaminoRewardValidatorTx is a transaction that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this transaction is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this transaction is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type CaminoRewardValidatorTx struct {
	Ins  []*avax.TransferableInput  `serialize:"true" json:"inputs"`
	Outs []*avax.TransferableOutput `serialize:"true" json:"outputs"`

	RewardValidatorTx `serialize:"true"`
}
