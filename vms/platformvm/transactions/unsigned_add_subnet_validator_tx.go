// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
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
func (tx *UnsignedAddSubnetValidatorTx) Verify(
	ctx *snow.Context,
	c codec.Manager,
	feeAmount uint64,
	feeAssetID ids.ID,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	duration := tx.Validator.Duration()
	switch {
	case duration < minStakeDuration: // Ensure staking length is not too short
		return ErrStakeTooShort
	case duration > maxStakeDuration: // Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if err := tx.BaseTx.Verify(ctx, c); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
