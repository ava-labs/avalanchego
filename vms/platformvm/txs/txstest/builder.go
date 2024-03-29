// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txstest

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	vmsigner "github.com/ava-labs/avalanchego/vms/platformvm/signer"
	walletsigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

func NewBuilder(
	ctx *snow.Context,
	cfg *config.Config,
	clk *mockable.Clock,
	state state.State,
) *Builder {
	return &Builder{
		ctx:   ctx,
		cfg:   cfg,
		clk:   clk,
		state: state,
	}
}

type Builder struct {
	ctx   *snow.Context
	cfg   *config.Config
	clk   *mockable.Clock
	state state.State
}

func (b *Builder) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewImportTx(
		chainID,
		to,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewExportTx(
		chainID,
		outputs,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building export tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewCreateChainTx(
		subnetID,
		genesis,
		vmID,
		fxIDs,
		chainName,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building create chain tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewCreateSubnetTx(
		owner,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building create subnet tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewTransformSubnetTx(
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
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewTransformSubnetTx(
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
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transform subnet tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewAddValidatorTx(
		vdr,
		rewardsOwner,
		shares,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer vmsigner.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewAddPermissionlessValidatorTx(
		vdr,
		signer,
		assetID,
		validationRewardsOwner,
		delegationRewardsOwner,
		shares,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewAddDelegatorTx(
		vdr,
		rewardsOwner,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add delegator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewAddPermissionlessDelegatorTx(
		vdr,
		assetID,
		rewardsOwner,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless delegator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewAddSubnetValidatorTx(
		vdr,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add subnet validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building remove subnet validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewTransferSubnetOwnershipTx(
		subnetID,
		owner,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transfer subnet ownership tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewBaseTx(
	outputs []*avax.TransferableOutput,
	keys []*secp256k1.PrivateKey,
	options ...common.Option,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)
	feeCalc, err := b.feeCalculator()
	if err != nil {
		return nil, err
	}

	utx, err := pBuilder.NewBaseTx(
		outputs,
		feeCalc,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building base tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) builders(keys []*secp256k1.PrivateKey) (builder.Builder, walletsigner.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrs   = kc.Addresses()
		backend = NewBackend(addrs, b.state, b.ctx.SharedMemory)
		context = NewContext(b.ctx, b.cfg, backend.state.GetTimestamp())
		builder = builder.New(addrs, context, backend)
		signer  = walletsigner.New(kc, backend)
	)

	return builder, signer
}

func (b *Builder) feeCalculator() (*fees.Calculator, error) {
	var (
		currentChainTime = b.state.GetTimestamp()
		isEActive        = b.cfg.IsEActivated(currentChainTime)
		feeCfg           = config.GetDynamicFeesConfig(isEActive)
	)

	nextChainTime, _, err := state.NextBlockTime(b.state, b.clk)
	if err != nil {
		return nil, fmt.Errorf("failed calculating next block time: %w", err)
	}

	feeManager, err := fees.UpdatedFeeManager(b.state, b.cfg, currentChainTime, nextChainTime)
	if err != nil {
		return nil, err
	}

	return &fees.Calculator{
		IsEActive:          isEActive,
		Config:             b.cfg,
		ChainTime:          nextChainTime,
		FeeManager:         feeManager,
		BlockMaxComplexity: feeCfg.BlockMaxComplexity,
	}, nil
}
