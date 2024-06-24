// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/subnet/manager"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

var (
	_ UnsignedTx = (*SetSubnetValidatorManagerTx)(nil)

	errCantParsePayload                = errors.New("cannot parse payload")
	errCantSetManagerForPrimaryNetwork = errors.New("cannot set manager for primary network")
)

type SetSubnetValidatorManagerTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// Warp message should include:
	//   - SubnetID
	//   - ChainID (where the validator manager lives)
	//   - Address (address of the validator manager)
	//   - BLS multisig over the above payload
	Message warp.Message `serialize:"true" json:"message"`
}

// InitCtx sets the FxID fields in the inputs and outputs of this
// [SetSubnetValidatorManagerTx]. Also sets the [ctx] to the given [vm.ctx] so
// that the addresses can be json marshalled into human readable format
func (tx *SetSubnetValidatorManagerTx) InitCtx(ctx *snow.Context) {
	tx.BaseTx.InitCtx(ctx)
}

func (tx *SetSubnetValidatorManagerTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified:
		// already passed syntactic verification
		return nil
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return err
	}

	var payload manager.SetSubnetValidatorManagerTxWarpMessagePayload
	_, err := manager.Codec.Unmarshal(tx.Message.Payload, &payload)
	if err != nil {
		return errCantParsePayload
	}
	if payload.SubnetID == constants.PrimaryNetworkID {
		return errCantSetManagerForPrimaryNetwork
	}

	tx.SyntacticallyVerified = true
	return nil
}

func (tx *SetSubnetValidatorManagerTx) Visit(visitor Visitor) error {
	return visitor.SetSubnetValidatorManagerTx(tx)
}
