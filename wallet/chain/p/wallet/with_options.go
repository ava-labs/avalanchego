// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wallet

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	vmsigner "github.com/ava-labs/avalanchego/vms/platformvm/signer"
	walletsigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

var _ Wallet = (*withOptions)(nil)

func WithOptions(
	wallet Wallet,
	options ...common.Option,
) Wallet {
	return &withOptions{
		wallet:  wallet,
		options: options,
	}
}

type withOptions struct {
	wallet  Wallet
	options []common.Option
}

func (w *withOptions) Builder() builder.Builder {
	return builder.WithOptions(
		w.wallet.Builder(),
		w.options...,
	)
}

func (w *withOptions) Signer() walletsigner.Signer {
	return w.wallet.Signer()
}

func (w *withOptions) IssueBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueBaseTx(
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddValidatorTx(
		vdr,
		rewardsOwner,
		shares,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddSubnetValidatorTx(
		vdr,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddDelegatorTx(
		vdr,
		rewardsOwner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueCreateSubnetTx(
		owner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueTransferSubnetOwnershipTx(
		subnetID,
		owner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueConvertSubnetToL1Tx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	validators []*txs.ConvertSubnetToL1Validator,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueConvertSubnetToL1Tx(
		subnetID,
		chainID,
		address,
		validators,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueRegisterL1ValidatorTx(
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
	message []byte,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueRegisterL1ValidatorTx(
		balance,
		proofOfPossession,
		message,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueSetL1ValidatorWeightTx(
	message []byte,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueSetL1ValidatorWeightTx(
		message,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueIncreaseL1ValidatorBalanceTx(
	validationID ids.ID,
	balance uint64,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueIncreaseL1ValidatorBalanceTx(
		validationID,
		balance,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueDisableL1ValidatorTx(
	validationID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueDisableL1ValidatorTx(
		validationID,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueImportTx(
		sourceChainID,
		to,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueExportTx(
		chainID,
		outputs,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTransformSubnetTx(
	subnetID ids.ID,
	assetID ids.ID,
	initialSupply uint64,
	maxSupply uint64,
	minConsumptionRate uint64,
	maxConsumptionRate uint64,
	minValidatorStake uint64,
	maxValidatorStake uint64,
	minStakeDuration time.Duration,
	maxStakeDuration time.Duration,
	minDelegationFee uint32,
	minDelegatorStake uint64,
	maxValidatorWeightFactor byte,
	uptimeRequirement uint32,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueTransformSubnetTx(
		subnetID,
		assetID,
		initialSupply,
		maxSupply,
		minConsumptionRate,
		maxConsumptionRate,
		minValidatorStake,
		maxValidatorStake,
		minStakeDuration,
		maxStakeDuration,
		minDelegationFee,
		minDelegatorStake,
		maxValidatorWeightFactor,
		uptimeRequirement,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer vmsigner.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...common.Option,
) (*txs.Tx, error) {
	return w.wallet.IssueUnsignedTx(
		utx,
		common.UnionOptions(w.options, options)...,
	)
}

func (w *withOptions) IssueTx(
	tx *txs.Tx,
	options ...common.Option,
) error {
	return w.wallet.IssueTx(
		tx,
		common.UnionOptions(w.options, options)...,
	)
}
