// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// EstimateAddPermissionlessValidatorTxComplexity returns the base complexity of
// an AddPermissionlessValidatorTx before inputs and outputs are known. This can
// be passed to Spend so that each party's fee share accounts for the
// transaction's intrinsic cost.
func EstimateAddPermissionlessValidatorTxComplexity(
	signer signer.Signer,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
) (gas.Dimensions, error) {
	signerComplexity, err := fee.SignerComplexity(signer)
	if err != nil {
		return gas.Dimensions{}, err
	}
	validatorOwnerComplexity, err := fee.OwnerComplexity(validationRewardsOwner)
	if err != nil {
		return gas.Dimensions{}, err
	}
	delegatorOwnerComplexity, err := fee.OwnerComplexity(delegationRewardsOwner)
	if err != nil {
		return gas.Dimensions{}, err
	}
	return fee.IntrinsicAddPermissionlessValidatorTxComplexities.Add(
		&signerComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
}

// ContributionComplexity calculates the total complexity of inputs and outputs
// from one or more SpendResults. This is used by the fee payer to account for
// other parties' complexity when calculating fees.
func ContributionComplexity(contributions ...*SpendResult) (gas.Dimensions, error) {
	var total gas.Dimensions
	for _, c := range contributions {
		if len(c.Inputs) > 0 {
			inputCx, err := fee.InputComplexity(c.Inputs...)
			if err != nil {
				return gas.Dimensions{}, err
			}
			total, err = total.Add(&inputCx)
			if err != nil {
				return gas.Dimensions{}, err
			}
		}
		allOutputs := make([]*avax.TransferableOutput, 0, len(c.ChangeOutputs)+len(c.StakeOutputs))
		allOutputs = append(allOutputs, c.ChangeOutputs...)
		allOutputs = append(allOutputs, c.StakeOutputs...)
		if len(allOutputs) > 0 {
			outputCx, err := fee.OutputComplexity(allOutputs...)
			if err != nil {
				return gas.Dimensions{}, err
			}
			total, err = total.Add(&outputCx)
			if err != nil {
				return gas.Dimensions{}, err
			}
		}
	}
	return total, nil
}

// NewAddPermissionlessValidatorTxFromParts assembles an
// AddPermissionlessValidatorTx from pre-selected inputs and outputs
// contributed by multiple parties.
//
// The caller is responsible for ensuring that the total inputs cover the
// required stake plus fees. The fee payer's contribution should include enough
// excess AVAX to cover the transaction fee.
func NewAddPermissionlessValidatorTxFromParts(
	context *Context,
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	contributions []*SpendResult,
	memo []byte,
) (*txs.AddPermissionlessValidatorTx, error) {
	var (
		inputs        []*avax.TransferableInput
		changeOutputs []*avax.TransferableOutput
		stakeOutputs  []*avax.TransferableOutput
	)
	for _, c := range contributions {
		inputs = append(inputs, c.Inputs...)
		changeOutputs = append(changeOutputs, c.ChangeOutputs...)
		stakeOutputs = append(stakeOutputs, c.StakeOutputs...)
	}

	// Ensure deterministic ordering
	utils.Sort(inputs)
	avax.SortTransferableOutputs(changeOutputs, txs.Codec)
	avax.SortTransferableOutputs(stakeOutputs, txs.Codec)

	// Preserve empty slices for JSON compatibility
	if inputs == nil {
		inputs = []*avax.TransferableInput{}
	}
	if changeOutputs == nil {
		changeOutputs = []*avax.TransferableOutput{}
	}
	if stakeOutputs == nil {
		stakeOutputs = []*avax.TransferableOutput{}
	}

	utils.Sort(validationRewardsOwner.Addrs)
	utils.Sort(delegationRewardsOwner.Addrs)

	tx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         changeOutputs,
			Memo:         memo,
		}},
		Validator:             vdr.Validator,
		Subnet:                vdr.Subnet,
		Signer:                signer,
		StakeOuts:             stakeOutputs,
		ValidatorRewardsOwner: validationRewardsOwner,
		DelegatorRewardsOwner: delegationRewardsOwner,
		DelegationShares:      shares,
	}
	return tx, initCtx(context, tx)
}
