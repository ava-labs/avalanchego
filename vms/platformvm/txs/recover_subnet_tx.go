// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
)

var (
	_ UnsignedTx = (*RecoverSubnetTx)(nil)

	ErrBalanceTooSmall = errors.New("balance of this validator is too low")

	errNoWeight       = errors.New("no weight")
	errPrimaryNetwork = errors.New("primary network")
)

type SubnetOnlyValidator struct {
	// Must be Ed25519 NodeID
	NodeID ids.NodeID `serialize:"true" json:"nodeID"`
	// Weight of this validator used when sampling
	Weight uint64 `serialize:"true" json:"weight"`
	// When this validator will stop validating the Subnet
	EndTime uint64 `serialize:"true" json:"endTime"`
	// Initial balance for this validator
	Balance uint64 `serialize:"true" json:"balance"`
	// [Signer] is the BLS key for this validator.
	// Note: We do not enforce that the BLS key is unique across all validators.
	//       This means that validators can share a key if they so choose.
	//       However, a NodeID + Subnet does uniquely map to a BLS key
	Signer signer.Signer `serialize:"true" json:"signer"`
	// Leftover $AVAX from the [Balance] will be issued to this
	// owner once it is removed from the validator set.
	ChangeOwner fx.Owner `serialize:"true" json:"changeOwner"`
}

type RecoverSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx
	// The subnet that is being recovered.
	Subnet ids.ID `serialize:"true" json:"subnetID"`
	// Validator set for the Subnet
	Validators []SubnetOnlyValidator `serialize:"true" json:"validators"`
	// Auth that will be allowing these validators into the network
	SubnetAuth verify.Verifiable `serialize:"true" json:"subnetAuthorization"`
}

// SyntacticVerify returns nil iff [tx] is valid
func (tx *RecoverSubnetTx) SyntacticVerify(ctx *snow.Context) error {
	switch {
	case tx == nil:
		return ErrNilTx
	case tx.SyntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.Subnet == constants.PrimaryNetworkID:
		return errPrimaryNetwork
	}

	if err := tx.BaseTx.SyntacticVerify(ctx); err != nil {
		return fmt.Errorf("failed to verify BaseTx: %w", err)
	}

	for idx, vdr := range tx.Validators {
		if vdr.NodeID == ids.EmptyNodeID {
			return fmt.Errorf("invalid params for validator %d: %w", idx, errEmptyNodeID)
		}

		if vdr.Weight == 0 {
			return fmt.Errorf("invalid params for validator %d: %w", idx, ErrWeightTooSmall)
		}

		if vdr.Balance < 5*units.Avax {
			return fmt.Errorf("invalid params for validator %d: %w", idx, ErrBalanceTooSmall)
		}

		if err := vdr.Signer.Verify(); err != nil {
			return fmt.Errorf("failed to verify signer for validator %d: %w", idx, vdr)
		}

		if err := vdr.ChangeOwner.Verify(); err != nil {
			return fmt.Errorf("failed to verify change owner for validator %d: %w", idx, vdr)
		}
	}

	// cache that this is valid
	tx.SyntacticallyVerified = true
	return nil
}

func (tx *RecoverSubnetTx) Visit(visitor Visitor) error {
	return visitor.RecoverSubnetTx(tx)
}
