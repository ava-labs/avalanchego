// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = &TransformSubnetTx{}
	_ secp256k1fx.UnsignedTx = &TransformSubnetTx{}

	errCantTransformPrimaryNetwork = errors.New("cannot transform primary network")
)

// TransformSubnetTx is an unsigned transformSubnetTx
type TransformSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet to transform
	SubnetID ids.ID `serialize:"true" json:"subnetID"`

	// TODO: provide staking information

	// Authorizes this transformation
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

func (tx *TransformSubnetTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.SubnetID == constants.PrimaryNetworkID:
		return errCantTransformPrimaryNetwork
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

func (tx *TransformSubnetTx) Visit(visitor Visitor) error {
	return visitor.TransformSubnetTx(tx)
}
