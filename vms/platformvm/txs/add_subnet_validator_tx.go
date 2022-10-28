// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/validator"
)

var (
	_ StakerTx = (*AddSubnetValidatorTx)(nil)

	errAddPrimaryNetworkValidator = errors.New("can't add primary network validator with AddSubnetValidatorTx")
)

// AddSubnetValidatorTx is an unsigned addSubnetValidatorTx
type AddSubnetValidatorTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// The validator
	Validator validator.SubnetValidator `serialize:"true" json:"validator"`
	// Auth that will be allowing this validator into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *AddSubnetValidatorTx) SubnetID() ids.ID     { return tx.Validator.Subnet }
func (tx *AddSubnetValidatorTx) NodeID() ids.NodeID   { return tx.Validator.NodeID }
func (tx *AddSubnetValidatorTx) StartTime() time.Time { return tx.Validator.StartTime() }
func (tx *AddSubnetValidatorTx) EndTime() time.Time   { return tx.Validator.EndTime() }
func (tx *AddSubnetValidatorTx) Weight() uint64       { return tx.Validator.Wght }

func (tx *AddSubnetValidatorTx) PendingPriority() Priority {
	return SubnetPermissionedValidatorPendingPriority
}

func (tx *AddSubnetValidatorTx) CurrentPriority() Priority {
	return SubnetPermissionedValidatorCurrentPriority
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *AddSubnetValidatorTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Validator.Subnet == constants.PrimaryNetworkID:
		return errAddPrimaryNetworkValidator
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

func (tx *AddSubnetValidatorTx) Visit(visitor Visitor) error {
	return visitor.AddSubnetValidatorTx(tx)
}
