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

var _ Builder = (*builder)(nil)

type Builder interface {
	AtomicTxBuilder
	DecisionTxBuilder
	ProposalTxBuilder
}

type AtomicTxBuilder interface {
	// chainID: chain to import UTXOs from
	// to: address of recipient
	// keys: keys to import the funds
	// changeAddr: address to send change to, if there is any
	NewImportTx(
		chainID ids.ID,
		to ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// amount: amount of tokens to export
	// chainID: chain to send the UTXOs to
	// to: address of recipient
	// keys: keys to pay the fee and provide the tokens
	// changeAddr: address to send change to, if there is any
	NewExportTx(
		amount uint64,
		chainID ids.ID,
		to ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)
}

type DecisionTxBuilder interface {
	// subnetID: ID of the subnet that validates the new chain
	// genesisData: byte repr. of genesis state of the new chain
	// vmID: ID of VM this chain runs
	// fxIDs: ids of features extensions this chain supports
	// chainName: name of the chain
	// keys: keys to sign the tx
	// changeAddr: address to send change to, if there is any
	NewCreateChainTx(
		subnetID ids.ID,
		genesisData []byte,
		vmID ids.ID,
		fxIDs []ids.ID,
		chainName string,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// threshold: [threshold] of [ownerAddrs] needed to manage this subnet
	// ownerAddrs: control addresses for the new subnet
	// keys: keys to pay the fee
	// changeAddr: address to send change to, if there is any
	NewCreateSubnetTx(
		threshold uint32,
		ownerAddrs []ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	NewTransformSubnetTx(
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
	) (*txs.Tx, error)

	// amount: amount the sender is sending
	// owner: recipient of the funds
	// keys: keys to sign the tx and pay the amount
	// changeAddr: address to send change to, if there is any
	NewBaseTx(
		amount uint64,
		owner secp256k1fx.OutputOwners,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)
}

type ProposalTxBuilder interface {
	// stakeAmount: amount the validator stakes
	// startTime: unix time they start validating
	// endTime: unix time they stop validating
	// nodeID: ID of the node we want to validate with
	// rewardAddress: address to send reward to, if applicable
	// shares: 10,000 times percentage of reward taken from delegators
	// keys: Keys providing the staked tokens
	// changeAddr: Address to send change to, if there is any
	NewAddValidatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		shares uint32,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// stakeAmount: amount the validator stakes
	// startTime: unix time they start validating
	// endTime: unix time they stop validating
	// nodeID: ID of the node we want to validate with
	// pop: the node proof of possession
	// rewardAddress: address to send reward to, if applicable
	// shares: 10,000 times percentage of reward taken from delegators
	// keys: Keys providing the staked tokens
	// changeAddr: Address to send change to, if there is any
	NewAddPermissionlessValidatorTx(
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
	) (*txs.Tx, error)

	// stakeAmount: amount the delegator stakes
	// startTime: unix time they start delegating
	// endTime: unix time they stop delegating
	// nodeID: ID of the node we are delegating to
	// rewardAddress: address to send reward to, if applicable
	// keys: keys providing the staked tokens
	// changeAddr: address to send change to, if there is any
	NewAddDelegatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// stakeAmount: amount the delegator stakes
	// startTime: unix time they start delegating
	// endTime: unix time they stop delegating
	// nodeID: ID of the node we are delegating to
	// rewardAddress: address to send reward to, if applicable
	// keys: keys providing the staked tokens
	// changeAddr: address to send change to, if there is any
	NewAddPermissionlessDelegatorTx(
		stakeAmount,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		rewardAddress ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// weight: sampling weight of the new validator
	// startTime: unix time they start delegating
	// endTime:  unix time they top delegating
	// nodeID: ID of the node validating
	// subnetID: ID of the subnet the validator will validate
	// keys: keys to use for adding the validator
	// changeAddr: address to send change to, if there is any
	NewAddSubnetValidatorTx(
		weight,
		startTime,
		endTime uint64,
		nodeID ids.NodeID,
		subnetID ids.ID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// Creates a transaction that removes [nodeID]
	// as a validator from [subnetID]
	// keys: keys to use for removing the validator
	// changeAddr: address to send change to, if there is any
	NewRemoveSubnetValidatorTx(
		nodeID ids.NodeID,
		subnetID ids.ID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)

	// Creates a transaction that transfers ownership of [subnetID]
	// threshold: [threshold] of [ownerAddrs] needed to manage this subnet
	// ownerAddrs: control addresses for the new subnet
	// keys: keys to use for modifying the subnet
	// changeAddr: address to send change to, if there is any
	NewTransferSubnetOwnershipTx(
		subnetID ids.ID,
		threshold uint32,
		ownerAddrs []ids.ShortID,
		keys []*secp256k1.PrivateKey,
		changeAddr ids.ShortID,
		memo []byte,
	) (*txs.Tx, error)
}

func New(
	ctx *snow.Context,
	cfg *config.Config,
	state state.State,
	atomicUTXOManager avax.AtomicUTXOManager,
) Builder {
	return &builder{
		ctx:     ctx,
		backend: NewBackend(cfg, state, atomicUTXOManager),
	}
}

type builder struct {
	ctx     *snow.Context
	backend *Backend
}

func (b *builder) NewImportTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building import tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

// TODO: should support other assets than AVAX
func (b *builder) NewExportTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building export tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewCreateChainTx(
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

	utx, err := pBuilder.NewCreateChainTx(subnetID, genesisData, vmID, fxIDs, chainName, options(changeAddr, memo)...)
	if err != nil {
		return nil, fmt.Errorf("failed building create chain tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewCreateSubnetTx(
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

	utx, err := pBuilder.NewCreateSubnetTx(subnetOwner, options(changeAddr, memo)...)
	if err != nil {
		return nil, fmt.Errorf("failed building create subnet tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewTransformSubnetTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transform subnet tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddValidatorTx(
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

	utx, err := pBuilder.NewAddValidatorTx(vdr, rewardOwner, shares, options(changeAddr, memo)...)
	if err != nil {
		return nil, fmt.Errorf("failed building add validator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddPermissionlessValidatorTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless validator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddDelegatorTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add delegator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddPermissionlessDelegatorTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add permissionless delegator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewAddSubnetValidatorTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building add subnet validator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewRemoveSubnetValidatorTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building remove subnet validator tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewTransferSubnetOwnershipTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building transfer subnet ownership tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) NewBaseTx(
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
		options(changeAddr, memo)...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed building base tx: %w", err)
	}

	tx, err := walletsigner.SignUnsigned(context.Background(), pSigner, utx)
	if err != nil {
		return nil, err
	}
	return tx, tx.SyntacticVerify(b.ctx)
}

func (b *builder) builders(keys []*secp256k1.PrivateKey) (walletbuilder.Builder, walletsigner.Signer) {
	var (
		kc      = secp256k1fx.NewKeychain(keys...)
		addrs   = kc.Addresses()
		context = &walletbuilder.Context{
			NetworkID:                     b.ctx.NetworkID,
			AVAXAssetID:                   b.ctx.AVAXAssetID,
			BaseTxFee:                     b.backend.cfg.TxFee,
			CreateSubnetTxFee:             b.backend.cfg.GetCreateSubnetTxFee(b.backend.state.GetTimestamp()),
			TransformSubnetTxFee:          b.backend.cfg.TransformSubnetTxFee,
			CreateBlockchainTxFee:         b.backend.cfg.GetCreateBlockchainTxFee(b.backend.state.GetTimestamp()),
			AddPrimaryNetworkValidatorFee: b.backend.cfg.AddPrimaryNetworkValidatorFee,
			AddPrimaryNetworkDelegatorFee: b.backend.cfg.AddPrimaryNetworkDelegatorFee,
			AddSubnetValidatorFee:         b.backend.cfg.AddSubnetValidatorFee,
			AddSubnetDelegatorFee:         b.backend.cfg.AddSubnetDelegatorFee,
		}
		builder = walletbuilder.New(addrs, context, b.backend)
		signer  = walletsigner.New(kc, b.backend)
	)
	b.backend.ResetAddresses(addrs)

	return builder, signer
}

func options(changeAddr ids.ShortID, memo []byte) []common.Option {
	return common.UnionOptions(
		[]common.Option{common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{changeAddr},
		})},
		[]common.Option{common.WithMemo(memo)},
	)
}
