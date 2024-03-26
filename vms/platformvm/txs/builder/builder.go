// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	vmsigner "github.com/ava-labs/avalanchego/vms/platformvm/signer"
	walletbuilder "github.com/ava-labs/avalanchego/wallet/chain/p/builder"
	walletsigner "github.com/ava-labs/avalanchego/wallet/chain/p/signer"
)

// Max number of items allowed in a page
const MaxPageSize = 1024

func New(
	ctx *snow.Context,
	cfg *config.Config,
	state state.State,
	atomicUTXOManager avax.AtomicUTXOManager,
) *Builder {
	return &Builder{
		ctx:     ctx,
		backend: NewBackend(cfg, state, atomicUTXOManager),
	}
}

type Builder struct {
	ctx     *snow.Context
	backend *Backend
}

func (b *Builder) NewImportTx(
	from ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	outOwner := &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     []ids.ShortID{to},
	}

	utx, err := pBuilder.NewImportTx(
		from,
		outOwner,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

// TODO: should support other assets than AVAX
func (b *Builder) NewExportTx(
	amount uint64,
	chainID ids.ID,
	to ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	outputs := []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: amount,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     []ids.ShortID{to},
			},
		},
	}}

	utx, err := pBuilder.NewExportTx(
		chainID,
		outputs,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building export tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewCreateChainTx(
	subnetID ids.ID,
	genesisData []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	utx, err := pBuilder.NewCreateChainTx(
		subnetID,
		genesisData,
		vmID,
		fxIDs,
		chainName,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building create chain tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewCreateSubnetTx(
	threshold uint32,
	ownerAddrs []ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	utils.Sort(ownerAddrs) // sort control addresses
	subnetOwner := &secp256k1fx.OutputOwners{
		Threshold: threshold,
		Addrs:     ownerAddrs,
	}

	utx, err := pBuilder.NewCreateSubnetTx(
		subnetOwner,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
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
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

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
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transform subnet tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	vdr := &txs.Validator{
		NodeID: nodeID,
		Start:  startTime,
		End:    endTime,
		Wght:   stakeAmount,
	}

	rewardOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardAddress},
	}

	utx, err := pBuilder.NewAddValidatorTx(
		vdr,
		rewardOwner,
		shares,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddPermissionlessValidatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	pop *vmsigner.ProofOfPossession,
	rewardAddress ids.ShortID,
	shares uint32,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	vdr := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmount,
		},
		Subnet: constants.PrimaryNetworkID,
	}

	rewardOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardAddress},
	}

	utx, err := pBuilder.NewAddPermissionlessValidatorTx(
		vdr,
		pop,
		b.ctx.AVAXAssetID,
		rewardOwner, // validationRewardsOwner
		rewardOwner, // delegationRewardsOwner
		shares,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddDelegatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	vdr := &txs.Validator{
		NodeID: nodeID,
		Start:  startTime,
		End:    endTime,
		Wght:   stakeAmount,
	}

	rewardOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardAddress},
	}

	utx, err := pBuilder.NewAddDelegatorTx(
		vdr,
		rewardOwner,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add delegator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddPermissionlessDelegatorTx(
	stakeAmount,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	rewardAddress ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	vdr := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   stakeAmount,
		},
		Subnet: constants.PrimaryNetworkID,
	}

	rewardOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{rewardAddress},
	}

	utx, err := pBuilder.NewAddPermissionlessDelegatorTx(
		vdr,
		b.ctx.AVAXAssetID,
		rewardOwner,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless delegator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewAddSubnetValidatorTx(
	weight,
	startTime,
	endTime uint64,
	nodeID ids.NodeID,
	subnetID ids.ID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	vdr := &txs.SubnetValidator{
		Validator: txs.Validator{
			NodeID: nodeID,
			Start:  startTime,
			End:    endTime,
			Wght:   weight,
		},
		Subnet: subnetID,
	}

	utx, err := pBuilder.NewAddSubnetValidatorTx(
		vdr,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
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
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	utx, err := pBuilder.NewRemoveSubnetValidatorTx(
		nodeID,
		subnetID,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building remove subnet validator tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	threshold uint32,
	ownerAddrs []ids.ShortID,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	utils.Sort(ownerAddrs) // sort control addresses
	newOwner := &secp256k1fx.OutputOwners{
		Threshold: threshold,
		Addrs:     ownerAddrs,
	}

	utx, err := pBuilder.NewTransferSubnetOwnershipTx(
		subnetID,
		newOwner,
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transfer subnet ownership tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) NewBaseTx(
	amount uint64,
	owner secp256k1fx.OutputOwners,
	keys []*secp256k1.PrivateKey,
	changeAddr ids.ShortID,
	memo []byte,
) (*txs.Tx, error) {
	pBuilder, pSigner := b.builders(keys)

	out := &avax.TransferableOutput{
		Asset: avax.Asset{ID: b.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt:          amount,
			OutputOwners: owner,
		},
	}

	utx, err := pBuilder.NewBaseTx(
		[]*avax.TransferableOutput{out},
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		}),
		common.WithMemo(memo),
	)
	if err != nil {
		return nil, fmt.Errorf("failed building base tx: %w", err)
	}

	return walletsigner.SignUnsigned(context.Background(), pSigner, utx)
}

func (b *Builder) builders(keys []*secp256k1.PrivateKey) (walletbuilder.Builder, walletsigner.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrs   = kc.Addresses()
		context = walletbuilder.NewContextFromConfig(b.ctx, b.backend.cfg, b.backend.state.GetTimestamp())
		builder = walletbuilder.New(addrs, context, b.backend)
		signer  = walletsigner.New(kc, b.backend)
	)
	b.backend.ResetAddresses(addrs)

	return builder, signer
}
