// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

// UnsignedRewardValidatorTx is a that represents a proposal to
// remove a validator that is currently validating from the validator set.
//
// If this is accepted and the next block accepted is a Commit
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX as well as a validating reward.
//
// If this is accepted and the next block accepted is an Abort
// block, the validator is removed and the address that the validator specified
// receives the staked AVAX but no reward.
type UnsignedRewardValidatorTx struct {
	avax.Metadata

	// ID of the tx that created the delegator/validator being removed/rewarded
	TxID ids.ID `serialize:"true" json:"txID"`

	// Marks if this validator should be rewarded according to this node.
	ShouldPreferCommit bool
}

func (tx *UnsignedRewardValidatorTx) SyntacticVerify(synCtx ProposalTxSyntacticVerificationContext,
) error {
	return nil
}
