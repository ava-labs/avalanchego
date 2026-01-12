// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
)

var (
	_ UnsignedTx = (*TransferSubnetOwnershipTx)(nil)

	ErrTransferPermissionlessSubnet = errors.New("cannot transfer ownership of a permissionless subnet")
)

type TransferSubnetOwnershipTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the subnet this tx is modifying
	Subnet ids.ID `serialize:"true" json:"subnetID"`
	// Proves that the issuer has the right to remove the node from the subnet.
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
	// Who is now authorized to manage this subnet
	Owner fx.Owner `serialize:"true" json:"newOwner"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [TransferSubnetOwnershipTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *TransferSubnetOwnershipTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
	tx.Owner.InitCtx(ctx)
}

func (tx *TransferSubnetOwnershipTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	case tx.Subnet == constants.PrimaryNetworkID:
		return ErrTransferPermissionlessSubnet
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}
	if err := verify.All(tx.SubnetAuth, tx.Owner); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *TransferSubnetOwnershipTx) Visit(visitor Visitor) error {
	return visitor.TransferSubnetOwnershipTx(tx)
}
