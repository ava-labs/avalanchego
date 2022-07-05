// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/reward"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ UnsignedTx             = &TransformSubnetTx{}
	_ secp256k1fx.UnsignedTx = &TransformSubnetTx{}

	errCantTransformPrimaryNetwork       = errors.New("cannot transform primary network")
	errEmptyAssetID                      = errors.New("empty asset ID is not valid")
	errAssetIDCantBeAVAX                 = errors.New("asset ID can't be AVAX")
	errInitialSupplyGreaterThanMaxSupply = errors.New("initial supply can't be greater than maximum supply")
	errMaxConsumptionRateTooLarge        = fmt.Errorf("max consumption rate must be less than or equal to %d", reward.PercentDenominator)
	errMinConsumptionRateTooLarge        = errors.New("min consumption rate must be less than or equal to max consumption rate")
)

// TransformSubnetTx is an unsigned transformSubnetTx
type TransformSubnetTx struct {
	// Metadata, inputs and outputs
	BaseTx `serialize:"true"`
	// ID of the Subnet to transform
	SubnetID ids.ID `serialize:"true" json:"subnetID"`
	// Asset to use when staking on the Subnet
	AssetID ids.ID `serialize:"true" json:"assetID"`
	// Amount to initially specify as the current supply
	InitialSupply uint64 `serialize:"true" json:"initialSupply"`
	// Amount to specify as the maximum token supply
	MaximumSupply uint64 `serialize:"true" json:"maximumSupply"`
	// MaxConsumptionRate is the rate to allocate funds if the validator's stake
	// duration is equal to the minting period
	MaxConsumptionRate uint64 `serialize:"true" json:"maxConsumptionRate"`
	// MinConsumptionRate is the rate to allocate funds if the validator's stake
	// duration is 0
	MinConsumptionRate uint64 `serialize:"true" json:"minConsumptionRate"`
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
	case tx.AssetID == ids.Empty:
		return errEmptyAssetID
	case tx.AssetID == ctx.AVAXAssetID:
		return errAssetIDCantBeAVAX
	case tx.InitialSupply > tx.MaximumSupply:
		return errInitialSupplyGreaterThanMaxSupply
	case tx.MaxConsumptionRate > reward.PercentDenominator:
		return errMaxConsumptionRateTooLarge
	case tx.MaxConsumptionRate < tx.MinConsumptionRate:
		return errMinConsumptionRateTooLarge
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
