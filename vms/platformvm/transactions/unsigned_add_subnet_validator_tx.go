// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/entities"
)

// UnsignedAddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type UnsignedAddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator entities.SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// Verify return nil iff [tx] is valid
func (tx *UnsignedAddSubnetValidatorTx) SyntacticVerify(synCtx ProposalTxSyntacticVerificationContext,
) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < synCtx.MinStakeDuration: // Ensure staking length is not too short
		return ErrStakeTooShort
	case duration > synCtx.MaxStakeDuration: // Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if err := tx.BaseTx.syntacticVerify(synCtx.Ctx, synCtx.C); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
