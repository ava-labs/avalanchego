// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	_ UnsignedTx = (*TransferSubnetOwnershipTx)(nil)

	ErrConvertPermissionlessSubnet = errors.New("cannot convert a permissionless subnet")
)

type ConvertSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx
	// ID of the Subnet to transform
	// Restrictions:
	// - Must not be the Primary Network ID
	Subnet ids.ID `json:"subnetID"`
	// Chain where the Subnet manager lives
	BlockchainID ids.ID `json:"blockchainID"`
	// Address of the Subnet manager
	Address []byte `json:"address"`
	// Authorizes this conversion
	SubnetAuth verify.Verifiable `json:"subnetAuthorization"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [ConvertSubnetTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *ConvertSubnetTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
}

func (tx *ConvertSubnetTx) SyntacticVerify(ctx *snow.Context) error {
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
	if err := tx.SubnetAuth.Verify(); err != nil {
		return err
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *ConvertSubnetTx) Visit(visitor Visitor) error {
	return visitor.ConvertSubnetTx(tx)
}
