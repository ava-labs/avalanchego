// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package unsigned

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/timed"

	pChainValidator "github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ Tx       = &AddSubnetValidatorTx{}
	_ timed.Tx = &AddSubnetValidatorTx{}

	ErrDSValidatorSubset = errors.New("staking period for all subnets must be a subset  of the primary network")
)

// AddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type AddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator pChainValidator.SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// StartTime of this validator
func (tx *AddSubnetValidatorTx) StartTime() time.Time {
	return tx.Validator.StartTime()
}

// EndTime of this validator
func (tx *AddSubnetValidatorTx) EndTime() time.Time {
	return tx.Validator.EndTime()
}

// Weight of this validator
func (tx *AddSubnetValidatorTx) Weight() uint64 {
	return tx.Validator.Weight()
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddSubnetValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := verify.All(&tx.Validator, tx.SubnetAuth); err != nil {
		return err
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}
