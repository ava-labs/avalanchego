// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var (
	ErrNoChangeAddress           = errors.New("no possible change address")
	ErrUnknownOutputType         = errors.New("unknown output type")
	ErrUnknownOwnerType          = errors.New("unknown owner type")
	ErrInsufficientAuthorization = errors.New("insufficient authorization")
	ErrInsufficientFunds         = errors.New("insufficient funds")

	_ Builder = (*builder)(nil)
)

// Builder provides a convenient interface for building unsigned P-chain
// transactions.
type Builder interface {
	// Context returns the configuration of the chain that this builder uses to
	// create transactions.
	Context() *Context

	// GetBalance calculates the amount of each asset that this builder has
	// control over.
	GetBalance(
		options ...common.Option,
	) (map[ids.ID]uint64, error)

	// GetImportableBalance calculates the amount of each asset that this
	// builder could import from the provided chain.
	//
	// - [chainID] specifies the chain the funds are from.
	GetImportableBalance(
		chainID ids.ID,
		options ...common.Option,
	) (map[ids.ID]uint64, error)

	// NewBaseTx creates a new simple value transfer.
	//
	// - [outputs] specifies all the recipients and amounts that should be sent
	//   from this transaction.
	NewBaseTx(
		outputs []*avax.TransferableOutput,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.BaseTx, error)

	// NewAddValidatorTx creates a new validator of the primary network.
	//
	// - [vdr] specifies all the details of the validation period such as the
	//   startTime, endTime, stake weight, and nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this validator
	//   may accrue during its validation period.
	// - [shares] specifies the fraction (out of 1,000,000) that this validator
	//   will take from delegation rewards. If 1,000,000 is provided, 100% of
	//   the delegation reward will be sent to the validator's [rewardsOwner].
	NewAddValidatorTx(
		vdr *txs.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.AddValidatorTx, error)

	// NewAddSubnetValidatorTx creates a new validator of a subnet.
	//
	// - [vdr] specifies all the details of the validation period such as the
	//   startTime, endTime, sampling weight, nodeID, and subnetID.
	NewAddSubnetValidatorTx(
		vdr *txs.SubnetValidator,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.AddSubnetValidatorTx, error)

	// NewRemoveSubnetValidatorTx removes [nodeID] from the validator
	// set [subnetID].
	NewRemoveSubnetValidatorTx(
		nodeID ids.NodeID,
		subnetID ids.ID,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.RemoveSubnetValidatorTx, error)

	// NewAddDelegatorTx creates a new delegator to a validator on the primary
	// network.
	//
	// - [vdr] specifies all the details of the delegation period such as the
	//   startTime, endTime, stake weight, and validator's nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this delegator
	//   may accrue at the end of its delegation period.
	NewAddDelegatorTx(
		vdr *txs.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.AddDelegatorTx, error)

	// NewCreateChainTx creates a new chain in the named subnet.
	//
	// - [subnetID] specifies the subnet to launch the chain in.
	// - [genesis] specifies the initial state of the new chain.
	// - [vmID] specifies the vm that the new chain will run.
	// - [fxIDs] specifies all the feature extensions that the vm should be
	//   running with.
	// - [chainName] specifies a human readable name for the chain.
	NewCreateChainTx(
		subnetID ids.ID,
		genesis []byte,
		vmID ids.ID,
		fxIDs []ids.ID,
		chainName string,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.CreateChainTx, error)

	// NewCreateSubnetTx creates a new subnet with the specified owner.
	//
	// - [owner] specifies who has the ability to create new chains and add new
	//   validators to the subnet.
	NewCreateSubnetTx(
		owner *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.CreateSubnetTx, error)

	// NewTransferSubnetOwnershipTx changes the owner of the named subnet.
	//
	// - [subnetID] specifies the subnet to be modified
	// - [owner] specifies who has the ability to create new chains and add new
	//   validators to the subnet.
	NewTransferSubnetOwnershipTx(
		subnetID ids.ID,
		owner *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.TransferSubnetOwnershipTx, error)

	// NewImportTx creates an import transaction that attempts to consume all
	// the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	NewImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.ImportTx, error)

	// NewExportTx creates an export transaction that attempts to send all the
	// provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	NewExportTx(
		chainID ids.ID,
		outputs []*avax.TransferableOutput,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.ExportTx, error)

	// NewTransformSubnetTx creates a transform subnet transaction that attempts
	// to convert the provided [subnetID] from a permissioned subnet to a
	// permissionless subnet. This transaction will convert
	// [maxSupply] - [initialSupply] of [assetID] to staking rewards.
	//
	// - [subnetID] specifies the subnet to transform.
	// - [assetID] specifies the asset to use to reward stakers on the subnet.
	// - [initialSupply] is the amount of [assetID] that will be in circulation
	//   after this transaction is accepted.
	// - [maxSupply] is the maximum total amount of [assetID] that should ever
	//   exist.
	// - [minConsumptionRate] is the rate that a staker will receive rewards
	//   if they stake with a duration of 0.
	// - [maxConsumptionRate] is the maximum rate that staking rewards should be
	//   consumed from the reward pool per year.
	// - [minValidatorStake] is the minimum amount of funds required to become a
	//   validator.
	// - [maxValidatorStake] is the maximum amount of funds a single validator
	//   can be allocated, including delegated funds.
	// - [minStakeDuration] is the minimum number of seconds a staker can stake
	//   for.
	// - [maxStakeDuration] is the maximum number of seconds a staker can stake
	//   for.
	// - [minValidatorStake] is the minimum amount of funds required to become a
	//   delegator.
	// - [maxValidatorWeightFactor] is the factor which calculates the maximum
	//   amount of delegation a validator can receive. A value of 1 effectively
	//   disables delegation.
	// - [uptimeRequirement] is the minimum percentage a validator must be
	//   online and responsive to receive a reward.
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
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.TransformSubnetTx, error)

	// NewAddPermissionlessValidatorTx creates a new validator of the specified
	// subnet.
	//
	// - [vdr] specifies all the details of the validation period such as the
	//   subnetID, startTime, endTime, stake weight, and nodeID.
	// - [signer] if the subnetID is the primary network, this is the BLS key
	//   for this validator. Otherwise, this value should be the empty signer.
	// - [assetID] specifies the asset to stake.
	// - [validationRewardsOwner] specifies the owner of all the rewards this
	//   validator earns for its validation period.
	// - [delegationRewardsOwner] specifies the owner of all the rewards this
	//   validator earns for delegations during its validation period.
	// - [shares] specifies the fraction (out of 1,000,000) that this validator
	//   will take from delegation rewards. If 1,000,000 is provided, 100% of
	//   the delegation reward will be sent to the validator's [rewardsOwner].
	NewAddPermissionlessValidatorTx(
		vdr *txs.SubnetValidator,
		signer signer.Signer,
		assetID ids.ID,
		validationRewardsOwner *secp256k1fx.OutputOwners,
		delegationRewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.AddPermissionlessValidatorTx, error)

	// NewAddPermissionlessDelegatorTx creates a new delegator of the specified
	// subnet on the specified nodeID.
	//
	// - [vdr] specifies all the details of the delegation period such as the
	//   subnetID, startTime, endTime, stake weight, and nodeID.
	// - [assetID] specifies the asset to stake.
	// - [rewardsOwner] specifies the owner of all the rewards this delegator
	//   earns during its delegation period.
	NewAddPermissionlessDelegatorTx(
		vdr *txs.SubnetValidator,
		assetID ids.ID,
		rewardsOwner *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.AddPermissionlessDelegatorTx, error)
}

type Backend interface {
	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetSubnetOwner(ctx context.Context, subnetID ids.ID) (fx.Owner, error)
}

type builder struct {
	addrs   set.Set[ids.ShortID]
	context *Context
	backend Backend
}

// New returns a new transaction builder.
//
//   - [addrs] is the set of addresses that the builder assumes can be used when
//     signing the transactions in the future.
//   - [context] provides the chain's configuration.
//   - [backend] provides the chain's state.
func New(
	addrs set.Set[ids.ShortID],
	context *Context,
	backend Backend,
) Builder {
	return &builder{
		addrs:   addrs,
		context: context,
		backend: backend,
	}
}

func (b *builder) Context() *Context {
	return b.context
}

func (b *builder) GetBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	ops := common.NewOptions(options)
	return b.getBalance(constants.PlatformChainID, ops)
}

func (b *builder) GetImportableBalance(
	chainID ids.ID,
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	ops := common.NewOptions(options)
	return b.getBalance(chainID, ops)
}

func (b *builder) NewBaseTx(
	outputs []*avax.TransferableOutput,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.BaseTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	utx := &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
			Outs:         outputs, // not sorted yet, we'll sort later on when we have all the outputs
		},
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.BaseTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	outputs = append(outputs, changeOuts...)
	avax.SortTransferableOutputs(outputs, txs.Codec)
	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddValidatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)

	utx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:        *vdr,
		RewardsOwner:     rewardsOwner,
		DelegationShares: shares,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		b.context.AVAXAssetID: vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.AddValidatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, stakeOutputs, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs
	utx.StakeOuts = stakeOutputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := b.authorizeSubnet(vdr.Subnet, ops)
	if err != nil {
		return nil, err
	}

	utx := &txs.AddSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		SubnetValidator: *vdr,
		SubnetAuth:      subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = fees.FinanceCredential(feeCalc, len(subnetAuth.SigIndices)); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.AddSubnetValidatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Subnet:     subnetID,
		NodeID:     nodeID,
		SubnetAuth: subnetAuth,
	}
	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = fees.FinanceCredential(feeCalc, len(subnetAuth.SigIndices)); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.RemoveSubnetValidatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddDelegatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)

	utx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:              *vdr,
		DelegationRewardsOwner: rewardsOwner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		b.context.AVAXAssetID: vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.AddDelegatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, stakeOutputs, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs
	utx.StakeOuts = stakeOutputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.CreateChainTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(fxIDs)

	utx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesis,
		SubnetAuth:  subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = fees.FinanceCredential(feeCalc, len(subnetAuth.SigIndices)); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	if err = feeCalc.CreateChainTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.CreateSubnetTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	utils.Sort(owner.Addrs)
	utx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Owner: owner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.CreateSubnetTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.TransferSubnetOwnershipTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(owner.Addrs)
	utx := &txs.TransferSubnetOwnershipTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Subnet:     subnetID,
		SubnetAuth: subnetAuth,
		Owner:      owner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = fees.FinanceCredential(feeCalc, len(subnetAuth.SigIndices)); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.TransferSubnetOwnershipTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.ImportTx, error) {
	ops := common.NewOptions(options)
	// 1. Build core transaction
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		SourceChain: sourceChainID,
	}

	// 2. Add imported inputs first
	importedUtxos, err := b.backend.UTXOs(ops.Context(), sourceChainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.context.AVAXAssetID

		importedInputs     = make([]*avax.TransferableInput, 0, len(importedUtxos))
		importedSigIndices = make([][]uint32, 0)
		importedAmounts    = make(map[ids.ID]uint64)
	)

	for _, utxo := range importedUtxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})

		assetID := utxo.AssetID()
		newImportedAmount, err := math.Add64(importedAmounts[assetID], out.Amt)
		if err != nil {
			return nil, err
		}
		importedAmounts[assetID] = newImportedAmount
		importedSigIndices = append(importedSigIndices, inputSigIndices)
	}
	if len(importedInputs) == 0 {
		return nil, fmt.Errorf(
			"%w: no UTXOs available to import",
			ErrInsufficientFunds,
		)
	}

	utils.Sort(importedInputs) // sort imported inputs
	utx.ImportedInputs = importedInputs

	// 3. Add an output for all non-avax denominated inputs.
	for assetID, amount := range importedAmounts {
		if assetID == avaxAssetID {
			// Avax-denominated inputs may be used to fully or partially pay fees,
			// so we'll handle them later on.
			continue
		}

		utx.Outs = append(utx.Outs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *to,
			},
		}) // we'll sort them later on
	}

	// 3. Finance fees as much as possible with imported, Avax-denominated UTXOs

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.ImportTx(utx); err != nil {
		return nil, err
	}

	for _, sigIndices := range importedSigIndices {
		if _, err = fees.FinanceCredential(feeCalc, len(sigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}
	}

	switch importedAVAX := importedAmounts[avaxAssetID]; {
	case importedAVAX == feeCalc.Fee:
		// imported inputs match exactly the fees to be paid
		avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
		return utx, b.initCtx(utx)

	case importedAVAX < feeCalc.Fee:
		// imported inputs can partially pay fees
		feeCalc.Fee -= importedAmounts[avaxAssetID]

	default:
		// imported inputs may be enough to pay taxes by themselves
		changeOut := &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				OutputOwners: *to, // we set amount after considering own fees
			},
		}

		// update fees to target given the extra output added
		_, outDimensions, err := fees.FinanceOutput(feeCalc, changeOut)
		if err != nil {
			return nil, fmt.Errorf("account for output fees: %w", err)
		}

		switch {
		case feeCalc.Fee < importedAVAX:
			changeOut.Out.(*secp256k1fx.TransferOutput).Amt = importedAVAX - feeCalc.Fee
			utx.Outs = append(utx.Outs, changeOut)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
			return utx, b.initCtx(utx)

		case feeCalc.Fee == importedAVAX:
			// imported fees pays exactly the tx cost. We don't include the outputs
			avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
			return utx, b.initCtx(utx)

		default:
			// imported avax are not enough to pay fees
			// Drop the changeOut and finance the tx
			if _, err := feeCalc.RemoveFeesFor(outDimensions, feeCalc.TipPercentage); err != nil {
				return nil, fmt.Errorf("failed reverting change output: %w", err)
			}
			feeCalc.Fee -= importedAVAX
		}
	}

	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{}
	inputs, changeOuts, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = append(utx.Outs, changeOuts...)
	avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
	return utx, b.initCtx(utx)
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.ExportTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	avax.SortTransferableOutputs(outputs, txs.Codec) // sort exported outputs

	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		DestinationChain: chainID,
		ExportedOutputs:  outputs,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.ExportTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = changeOuts

	return utx, b.initCtx(utx)
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
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.TransformSubnetTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utx := &txs.TransformSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Subnet:                   subnetID,
		AssetID:                  assetID,
		InitialSupply:            initialSupply,
		MaximumSupply:            maxSupply,
		MinConsumptionRate:       minConsumptionRate,
		MaxConsumptionRate:       maxConsumptionRate,
		MinValidatorStake:        minValidatorStake,
		MaxValidatorStake:        maxValidatorStake,
		MinStakeDuration:         uint32(minStakeDuration / time.Second),
		MaxStakeDuration:         uint32(maxStakeDuration / time.Second),
		MinDelegationFee:         minDelegationFee,
		MinDelegatorStake:        minDelegatorStake,
		MaxValidatorWeightFactor: maxValidatorWeightFactor,
		UptimeRequirement:        uptimeRequirement,
		SubnetAuth:               subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{
		assetID: maxSupply - initialSupply,
	} // fees are calculated in financeTx

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = fees.FinanceCredential(feeCalc, len(subnetAuth.SigIndices)); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
	}

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.TransformSubnetTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddPermissionlessValidatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(validationRewardsOwner.Addrs)
	utils.Sort(delegationRewardsOwner.Addrs)

	utx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:             vdr.Validator,
		Subnet:                vdr.Subnet,
		Signer:                signer,
		ValidatorRewardsOwner: validationRewardsOwner,
		DelegatorRewardsOwner: delegationRewardsOwner,
		DelegationShares:      shares,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.AddPermissionlessValidatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, stakeOutputs, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs
	utx.StakeOuts = stakeOutputs

	return utx, b.initCtx(utx)
}

func (b *builder) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddPermissionlessDelegatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)

	utx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:              vdr.Validator,
		Subnet:                 vdr.Subnet,
		DelegationRewardsOwner: rewardsOwner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
	if err := feeCalc.AddPermissionlessDelegatorTx(utx); err != nil {
		return nil, err
	}

	inputs, outputs, stakeOutputs, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = outputs
	utx.StakeOuts = stakeOutputs

	return utx, b.initCtx(utx)
}

func (b *builder) getBalance(
	chainID ids.ID,
	options *common.Options,
) (
	balance map[ids.ID]uint64,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), chainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(b.addrs)
	minIssuanceTime := options.MinIssuanceTime()
	balance = make(map[ids.ID]uint64)

	// Iterate over the UTXOs
	for _, utxo := range utxos {
		outIntf := utxo.Out
		if lockedOut, ok := outIntf.(*stakeable.LockOut); ok {
			if !options.AllowStakeableLocked() && lockedOut.Locktime > minIssuanceTime {
				// This output is currently locked, so this output can't be
				// burned.
				continue
			}
			outIntf = lockedOut.TransferableOut
		}

		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, ErrUnknownOutputType
		}

		_, ok = common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		assetID := utxo.AssetID()
		balance[assetID], err = math.Add64(balance[assetID], out.Amt)
		if err != nil {
			return nil, err
		}
	}
	return balance, nil
}

// spend takes in the requested burn amounts and the requested stake amounts.
//
//   - [amountsToBurn] maps assetID to the amount of the asset to spend without
//     producing an output. This is typically used for fees. However, it can
//     also be used to consume some of an asset that will be produced in
//     separate outputs, such as ExportedOutputs. Only unlocked UTXOs are able
//     to be burned here.
//   - [amountsToStake] maps assetID to the amount of the asset to spend and
//     place into the staked outputs. First locked UTXOs are attempted to be
//     used for these funds, and then unlocked UTXOs will be attempted to be
//     used. There is no preferential ordering on the unlock times.
func (b *builder) financeTx(
	amountsToBurn map[ids.ID]uint64,
	amountsToStake map[ids.ID]uint64,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	changeOutputs []*avax.TransferableOutput,
	stakeOutputs []*avax.TransferableOutput,
	err error,
) {
	avaxAssetID := b.context.AVAXAssetID
	utxos, err := b.backend.UTXOs(options.Context(), constants.PlatformChainID)
	if err != nil {
		return nil, nil, nil, err
	}

	// we can only pay fees in avax, so we sort avax-denominated UTXOs last
	// to maximize probability of being able to pay fees.
	slices.SortFunc(utxos, func(lhs, rhs *avax.UTXO) int {
		switch {
		case lhs.Asset.AssetID() == avaxAssetID && rhs.Asset.AssetID() != avaxAssetID:
			return 1
		case lhs.Asset.AssetID() != avaxAssetID && rhs.Asset.AssetID() == avaxAssetID:
			return -1
		default:
			return 0
		}
	})

	addrs := options.Addresses(b.addrs)
	minIssuanceTime := options.MinIssuanceTime()

	addr, ok := addrs.Peek()
	if !ok {
		return nil, nil, nil, ErrNoChangeAddress
	}
	changeOwner := options.ChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	})

	amountsToBurn[avaxAssetID] += feeCalc.Fee

	// Initialize the return values with empty slices to preserve backward
	// compatibility of the json representation of transactions with no
	// inputs or outputs.
	inputs = make([]*avax.TransferableInput, 0)
	changeOutputs = make([]*avax.TransferableOutput, 0)
	stakeOutputs = make([]*avax.TransferableOutput, 0)

	// Iterate over the locked UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()

		// If we have staked enough of the asset, then we have no need burn
		// more.
		if amountsToStake[assetID] == 0 {
			continue
		}

		outIntf := utxo.Out
		lockedOut, ok := outIntf.(*stakeable.LockOut)
		if !ok {
			// This output isn't locked, so it will be handled during the next
			// iteration of the UTXO set
			continue
		}
		if minIssuanceTime >= lockedOut.Locktime {
			// This output isn't locked, so it will be handled during the next
			// iteration of the UTXO set
			continue
		}

		out, ok := lockedOut.TransferableOut.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, nil, nil, ErrUnknownOutputType
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		input := &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &stakeable.LockIn{
				Locktime: lockedOut.Locktime,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt: out.Amt,
					Input: secp256k1fx.Input{
						SigIndices: inputSigIndices,
					},
				},
			},
		}

		addedFees, err := fees.FinanceInput(feeCalc, input)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		addedFees, err = fees.FinanceCredential(feeCalc, len(inputSigIndices))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		inputs = append(inputs, input)

		// Stake any value that should be staked
		amountToStake := min(
			amountsToStake[assetID], // Amount we still need to stake
			out.Amt,                 // Amount available to stake
		)

		// Add the output to the staked outputs
		stakeOut := &avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &stakeable.LockOut{
				Locktime: lockedOut.Locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          amountToStake,
					OutputOwners: out.OutputOwners,
				},
			},
		}

		addedFees, _, err = fees.FinanceOutput(feeCalc, stakeOut)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		stakeOutputs = append(stakeOutputs, stakeOut)

		amountsToStake[assetID] -= amountToStake
		if remainingAmount := out.Amt - amountToStake; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned
			changeOut := &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &stakeable.LockOut{
					Locktime: lockedOut.Locktime,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          remainingAmount,
						OutputOwners: out.OutputOwners,
					},
				},
			}

			// update fees to account for the change output
			addedFees, _, err = fees.FinanceOutput(feeCalc, changeOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
			}
			amountsToBurn[avaxAssetID] += addedFees

			changeOutputs = append(changeOutputs, changeOut)
		}
	}

	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if amountsToStake[assetID] == 0 && amountsToBurn[assetID] == 0 {
			continue
		}

		outIntf := utxo.Out
		if lockedOut, ok := outIntf.(*stakeable.LockOut); ok {
			if lockedOut.Locktime > minIssuanceTime {
				// This output is currently locked, so this output can't be
				// burned.
				continue
			}
			outIntf = lockedOut.TransferableOut
		}

		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, nil, nil, ErrUnknownOutputType
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		input := &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		}

		addedFees, err := fees.FinanceInput(feeCalc, input)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		addedFees, err = fees.FinanceCredential(feeCalc, len(inputSigIndices))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for credential fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		inputs = append(inputs, input)

		// Stake any value that should be staked
		amountToStake := min(
			amountsToStake[assetID], // Amount we still need to stake
			out.Amt,                 // Amount available to stake
		)
		amountsToStake[assetID] -= amountToStake
		if amountToStake > 0 {
			// Some of this input was put for staking
			stakeOut := &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					Amt:          amountToStake,
					OutputOwners: *changeOwner,
				},
			}

			stakeOutputs = append(stakeOutputs, stakeOut)

			addedFees, _, err = fees.FinanceOutput(feeCalc, stakeOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
			}
			amountsToBurn[avaxAssetID] += addedFees
		}

		// Burn any value that should be burned
		amountAvalibleToBurn := out.Amt - amountToStake
		amountToBurn := min(
			amountsToBurn[assetID], // Amount we still need to burn
			amountAvalibleToBurn,   // Amount available to burn
		)
		amountsToBurn[assetID] -= amountToBurn
		if remainingAmount := amountAvalibleToBurn - amountToBurn; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned, once fees are removed
			changeOut := &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: *changeOwner,
				},
			}

			// update fees to account for the change output
			addedFees, _, err = fees.FinanceOutput(feeCalc, changeOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
			}

			if assetID != avaxAssetID {
				changeOut.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmount
				amountsToBurn[avaxAssetID] += addedFees
				changeOutputs = append(changeOutputs, changeOut)
			} else {
				// here assetID == b.context.AVAXAssetID
				switch {
				case addedFees < remainingAmount:
					changeOut.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmount - addedFees
					changeOutputs = append(changeOutputs, changeOut)
				case addedFees >= remainingAmount:
					amountsToBurn[assetID] += addedFees - remainingAmount
				}
			}
		}
	}

	for assetID, amount := range amountsToStake {
		if amount != 0 {
			return nil, nil, nil, fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q to stake",
				ErrInsufficientFunds,
				amount,
				assetID,
			)
		}
	}
	for assetID, amount := range amountsToBurn {
		if amount != 0 {
			return nil, nil, nil, fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q",
				ErrInsufficientFunds,
				amount,
				assetID,
			)
		}
	}

	utils.Sort(inputs)                                     // sort inputs
	avax.SortTransferableOutputs(changeOutputs, txs.Codec) // sort the change outputs
	avax.SortTransferableOutputs(stakeOutputs, txs.Codec)  // sort stake outputs
	return inputs, changeOutputs, stakeOutputs, nil
}

func (b *builder) authorizeSubnet(
	subnetID ids.ID,
	options *common.Options,
) (*secp256k1fx.Input, error) {
	ownerIntf, err := b.backend.GetSubnetOwner(options.Context(), subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch subnet owner for %q: %w",
			subnetID,
			err,
		)
	}
	owner, ok := ownerIntf.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, ErrUnknownOwnerType
	}

	addrs := options.Addresses(b.addrs)
	minIssuanceTime := options.MinIssuanceTime()
	inputSigIndices, ok := common.MatchOwners(owner, addrs, minIssuanceTime)
	if !ok {
		// We can't authorize the subnet
		return nil, ErrInsufficientAuthorization
	}
	return &secp256k1fx.Input{
		SigIndices: inputSigIndices,
	}, nil
}

func (b *builder) initCtx(tx txs.UnsignedTx) error {
	ctx, err := NewSnowContext(b.context.NetworkID, b.context.AVAXAssetID)
	if err != nil {
		return err
	}

	tx.InitCtx(ctx)
	return nil
}
