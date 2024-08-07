// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	feecomponent "github.com/ava-labs/avalanchego/vms/components/fee"
	txfee "github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
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
		options ...common.Option,
	) (*txs.AddValidatorTx, error)

	// NewAddSubnetValidatorTx creates a new validator of a subnet.
	//
	// - [vdr] specifies all the details of the validation period such as the
	//   startTime, endTime, sampling weight, nodeID, and subnetID.
	NewAddSubnetValidatorTx(
		vdr *txs.SubnetValidator,
		options ...common.Option,
	) (*txs.AddSubnetValidatorTx, error)

	// NewRemoveSubnetValidatorTx removes [nodeID] from the validator
	// set [subnetID].
	NewRemoveSubnetValidatorTx(
		nodeID ids.NodeID,
		subnetID ids.ID,
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
		options ...common.Option,
	) (*txs.CreateChainTx, error)

	// NewCreateSubnetTx creates a new subnet with the specified owner.
	//
	// - [owner] specifies who has the ability to create new chains and add new
	//   validators to the subnet.
	NewCreateSubnetTx(
		owner *secp256k1fx.OutputOwners,
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
	options ...common.Option,
) (*txs.BaseTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.TxFee,
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	outputComplexity, err := txfee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicBaseTxComplexities.Add(
		&memoComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, changeOutputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}
	outputs = append(outputs, changeOutputs...)
	avax.SortTransferableOutputs(outputs, txs.Codec) // sort the outputs

	tx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    b.context.NetworkID,
		BlockchainID: constants.PlatformChainID,
		Ins:          inputs,
		Outs:         outputs,
		Memo:         memo,
	}}
	return tx, b.initCtx(tx)
}

func (b *builder) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.AddValidatorTx, error) {
	avaxAssetID := b.context.AVAXAssetID
	toBurn := map[ids.ID]uint64{
		avaxAssetID: b.context.StaticFeeConfig.AddPrimaryNetworkValidatorFee,
	}
	toStake := map[ids.ID]uint64{
		avaxAssetID: vdr.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		feecomponent.Dimensions{},
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	utils.Sort(rewardsOwner.Addrs)
	tx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         ops.Memo(),
		}},
		Validator:        *vdr,
		StakeOuts:        stakeOutputs,
		RewardsOwner:     rewardsOwner,
		DelegationShares: shares,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	options ...common.Option,
) (*txs.AddSubnetValidatorTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.AddSubnetValidatorFee,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(vdr.Subnet, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := txfee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicAddSubnetValidatorTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	tx := &txs.AddSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		SubnetValidator: *vdr,
		SubnetAuth:      subnetAuth,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	options ...common.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.TxFee,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := txfee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicRemoveSubnetValidatorTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	tx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Subnet:     subnetID,
		NodeID:     nodeID,
		SubnetAuth: subnetAuth,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.AddDelegatorTx, error) {
	avaxAssetID := b.context.AVAXAssetID
	toBurn := map[ids.ID]uint64{
		avaxAssetID: b.context.StaticFeeConfig.AddPrimaryNetworkDelegatorFee,
	}
	toStake := map[ids.ID]uint64{
		avaxAssetID: vdr.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(toBurn, toStake, 0, feecomponent.Dimensions{}, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(rewardsOwner.Addrs)
	tx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         ops.Memo(),
		}},
		Validator:              *vdr,
		StakeOuts:              stakeOutputs,
		DelegationRewardsOwner: rewardsOwner,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (*txs.CreateChainTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.CreateBlockchainTxFee,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	bandwidth, err := math.Mul(uint64(len(fxIDs)), ids.IDLen)
	if err != nil {
		return nil, err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(chainName)))
	if err != nil {
		return nil, err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(genesis)))
	if err != nil {
		return nil, err
	}
	bandwidth, err = math.Add(bandwidth, uint64(len(memo)))
	if err != nil {
		return nil, err
	}
	dynamicComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: bandwidth,
	}
	authComplexity, err := txfee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicCreateChainTxComplexities.Add(
		&dynamicComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(fxIDs)
	tx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesis,
		SubnetAuth:  subnetAuth,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.CreateSubnetTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.CreateSubnetTxFee,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	ownerComplexity, err := txfee.OwnerComplexity(owner)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicCreateSubnetTxComplexities.Add(
		&memoComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(owner.Addrs)
	tx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		Owner: owner,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewTransferSubnetOwnershipTx(
	subnetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.TransferSubnetOwnershipTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.TxFee,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := txfee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	ownerComplexity, err := txfee.OwnerComplexity(owner)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicTransferSubnetOwnershipTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(owner.Addrs)
	tx := &txs.TransferSubnetOwnershipTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		Subnet:     subnetID,
		Owner:      owner,
		SubnetAuth: subnetAuth,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.ImportTx, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), sourceChainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.context.AVAXAssetID
		txFee           = b.context.StaticFeeConfig.TxFee

		importedInputs  = make([]*avax.TransferableInput, 0, len(utxos))
		importedAmounts = make(map[ids.ID]uint64)
	)
	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
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
		newImportedAmount, err := math.Add(importedAmounts[assetID], out.Amt)
		if err != nil {
			return nil, err
		}
		importedAmounts[assetID] = newImportedAmount
	}
	utils.Sort(importedInputs) // sort imported inputs

	if len(importedInputs) == 0 {
		return nil, fmt.Errorf(
			"%w: no UTXOs available to import",
			ErrInsufficientFunds,
		)
	}

	outputs := make([]*avax.TransferableOutput, 0, len(importedAmounts))
	for assetID, amount := range importedAmounts {
		if assetID == avaxAssetID {
			continue
		}

		outputs = append(outputs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *to,
			},
		})
	}

	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	inputComplexity, err := txfee.InputComplexity(importedInputs...)
	if err != nil {
		return nil, err
	}
	outputComplexity, err := txfee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicImportTxComplexities.Add(
		&memoComplexity,
		&inputComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	var (
		toBurn    map[ids.ID]uint64
		toStake   = map[ids.ID]uint64{}
		excessFee uint64
	)
	if importedAVAX := importedAmounts[avaxAssetID]; importedAVAX > txFee {
		toBurn = map[ids.ID]uint64{}
		excessFee = importedAVAX - txFee
	} else {
		toBurn = map[ids.ID]uint64{
			avaxAssetID: txFee - importedAVAX,
		}
		excessFee = 0
	}

	inputs, changeOutputs, _, err := b.spend(toBurn, toStake, excessFee, complexity, to, ops)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}
	outputs = append(outputs, changeOutputs...)

	avax.SortTransferableOutputs(outputs, txs.Codec) // sort imported outputs
	tx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		SourceChain:    sourceChainID,
		ImportedInputs: importedInputs,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.ExportTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.TxFee,
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	outputComplexity, err := txfee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicExportTxComplexities.Add(
		&memoComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, changeOutputs, _, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	avax.SortTransferableOutputs(outputs, txs.Codec) // sort exported outputs
	tx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         changeOutputs,
			Memo:         memo,
		}},
		DestinationChain: chainID,
		ExportedOutputs:  outputs,
	}
	return tx, b.initCtx(tx)
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
	options ...common.Option,
) (*txs.TransformSubnetTx, error) {
	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.StaticFeeConfig.TransformSubnetTxFee,
		assetID:               maxSupply - initialSupply,
	}
	toStake := map[ids.ID]uint64{}
	inputs, outputs, _, err := b.spend(toBurn, toStake, 0, feecomponent.Dimensions{}, nil, ops)
	if err != nil {
		return nil, err
	}

	tx := &txs.TransformSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
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
	return tx, b.initCtx(tx)
}

func (b *builder) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*txs.AddPermissionlessValidatorTx, error) {
	avaxAssetID := b.context.AVAXAssetID
	toBurn := map[ids.ID]uint64{}
	if vdr.Subnet == constants.PrimaryNetworkID {
		toBurn[avaxAssetID] = b.context.StaticFeeConfig.AddPrimaryNetworkValidatorFee
	} else {
		toBurn[avaxAssetID] = b.context.StaticFeeConfig.AddSubnetValidatorFee
	}
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	signerComplexity, err := txfee.SignerComplexity(signer)
	if err != nil {
		return nil, err
	}
	validatorOwnerComplexity, err := txfee.OwnerComplexity(validationRewardsOwner)
	if err != nil {
		return nil, err
	}
	delegatorOwnerComplexity, err := txfee.OwnerComplexity(delegationRewardsOwner)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicAddPermissionlessValidatorTxComplexities.Add(
		&memoComplexity,
		&signerComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, baseOutputs, stakeOutputs, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(validationRewardsOwner.Addrs)
	utils.Sort(delegationRewardsOwner.Addrs)
	tx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
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
	return tx, b.initCtx(tx)
}

func (b *builder) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.AddPermissionlessDelegatorTx, error) {
	avaxAssetID := b.context.AVAXAssetID
	toBurn := map[ids.ID]uint64{}
	if vdr.Subnet == constants.PrimaryNetworkID {
		toBurn[avaxAssetID] = b.context.StaticFeeConfig.AddPrimaryNetworkDelegatorFee
	} else {
		toBurn[avaxAssetID] = b.context.StaticFeeConfig.AddSubnetDelegatorFee
	}
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := feecomponent.Dimensions{
		feecomponent.Bandwidth: uint64(len(memo)),
	}
	ownerComplexity, err := txfee.OwnerComplexity(rewardsOwner)
	if err != nil {
		return nil, err
	}
	complexity, err := txfee.IntrinsicAddPermissionlessDelegatorTxComplexities.Add(
		&memoComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}
	inputs, baseOutputs, stakeOutputs, err := b.spend(toBurn, toStake, 0, complexity, nil, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(rewardsOwner.Addrs)
	tx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         memo,
		}},
		Validator:              vdr.Validator,
		Subnet:                 vdr.Subnet,
		StakeOuts:              stakeOutputs,
		DelegationRewardsOwner: rewardsOwner,
	}
	return tx, b.initCtx(tx)
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
		balance[assetID], err = math.Add(balance[assetID], out.Amt)
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
func (b *builder) spend(
	amountsToBurn map[ids.ID]uint64,
	amountsToStake map[ids.ID]uint64,
	excessFee uint64,
	complexity feecomponent.Dimensions,
	ownerOverride *secp256k1fx.OutputOwners,
	options *common.Options,
) (
	[]*avax.TransferableInput,
	[]*avax.TransferableOutput,
	[]*avax.TransferableOutput,
	error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), constants.PlatformChainID)
	if err != nil {
		return nil, nil, nil, err
	}

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
	if ownerOverride == nil {
		ownerOverride = changeOwner
	}

	s := spendHelper{
		weights:  b.context.ComplexityWeights,
		gasPrice: b.context.GasPrice,

		amountsToBurn:  amountsToBurn,
		amountsToStake: amountsToStake,
		complexity:     complexity,

		// Initialize the return values with empty slices to preserve backward
		// compatibility of the json representation of transactions with no
		// inputs or outputs.
		inputs:        make([]*avax.TransferableInput, 0),
		changeOutputs: make([]*avax.TransferableOutput, 0),
		stakeOutputs:  make([]*avax.TransferableOutput, 0),
	}

	lockedUTXOs, unlockedUTXOs := splitLockedStakeableUTXOs(utxos, minIssuanceTime)
	for _, utxo := range lockedUTXOs {
		assetID := utxo.AssetID()
		if !s.shouldConsumeLockedAsset(assetID) {
			continue
		}

		out, locktime, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, nil, nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &stakeable.LockIn{
				Locktime: locktime,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt: out.Amt,
					Input: secp256k1fx.Input{
						SigIndices: inputSigIndices,
					},
				},
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}

		excess := s.consumeLockedAsset(assetID, out.Amt)
		err = s.addStakedOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &stakeable.LockOut{
				Locktime: locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          out.Amt - excess,
					OutputOwners: out.OutputOwners,
				},
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}

		if excess == 0 {
			continue
		}

		// This input had extra value, so some of it must be returned
		err = s.addChangeOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &stakeable.LockOut{
				Locktime: locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          excess,
					OutputOwners: out.OutputOwners,
				},
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// Add all the remaining stake amounts assuming unlocked UTXOs.
	for assetID, amount := range s.amountsToStake {
		if amount == 0 {
			continue
		}

		err = s.addStakedOutput(&avax.TransferableOutput{
			Asset: avax.Asset{
				ID: assetID,
			},
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *changeOwner,
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	// AVAX is handled last to account for fees.
	avaxUTXOs, nonAVAXUTXOs := splitAVAXUTXOs(unlockedUTXOs, b.context.AVAXAssetID)
	for _, utxo := range nonAVAXUTXOs {
		assetID := utxo.AssetID()
		if !s.shouldConsumeAsset(assetID) {
			continue
		}

		out, _, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, nil, nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}

		excess := s.consumeAsset(assetID, out.Amt)
		if excess == 0 {
			continue
		}

		// This input had extra value, so some of it must be returned
		err = s.addChangeOutput(&avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &secp256k1fx.TransferOutput{
				Amt:          excess,
				OutputOwners: *changeOwner,
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}
	}

	for _, utxo := range avaxUTXOs {
		requiredFee, err := s.calculateFee()
		if err != nil {
			return nil, nil, nil, err
		}

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if !s.shouldConsumeAsset(b.context.AVAXAssetID) && excessFee >= requiredFee {
			break
		}

		out, _, err := unwrapOutput(utxo.Out)
		if err != nil {
			return nil, nil, nil, err
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		err = s.addInput(&avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})
		if err != nil {
			return nil, nil, nil, err
		}

		excess := s.consumeAsset(b.context.AVAXAssetID, out.Amt)
		excessFee, err = math.Add(excessFee, excess)
		if err != nil {
			return nil, nil, nil, err
		}

		// If we need to consume additional AVAX, we should be returning the
		// change to the change address.
		ownerOverride = changeOwner
	}

	if err := s.verifyAssetsConsumed(); err != nil {
		return nil, nil, nil, err
	}

	requiredFee, err := s.calculateFee()
	if err != nil {
		return nil, nil, nil, err
	}
	if excessFee < requiredFee {
		return nil, nil, nil, fmt.Errorf(
			"%w: provided UTXOs need %d more units of asset %q",
			ErrInsufficientFunds,
			requiredFee-excessFee,
			b.context.AVAXAssetID,
		)
	}

	secpOutput := &secp256k1fx.TransferOutput{
		Amt:          0, // Populated later if used
		OutputOwners: *ownerOverride,
	}
	newOutput := &avax.TransferableOutput{
		Asset: avax.Asset{
			ID: b.context.AVAXAssetID,
		},
		Out: secpOutput,
	}
	if err := s.addOutputComplexity(newOutput); err != nil {
		return nil, nil, nil, err
	}

	requiredFeeWithChange, err := s.calculateFee()
	if err != nil {
		return nil, nil, nil, err
	}
	if excessFee > requiredFeeWithChange {
		// It is worth adding the change output
		secpOutput.Amt = excessFee - requiredFeeWithChange
		s.changeOutputs = append(s.changeOutputs, newOutput)
	}

	utils.Sort(s.inputs)                                     // sort inputs
	avax.SortTransferableOutputs(s.changeOutputs, txs.Codec) // sort the change outputs
	avax.SortTransferableOutputs(s.stakeOutputs, txs.Codec)  // sort stake outputs
	return s.inputs, s.changeOutputs, s.stakeOutputs, nil
}

func (b *builder) authorizeSubnet(subnetID ids.ID, options *common.Options) (*secp256k1fx.Input, error) {
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

type spendHelper struct {
	weights  feecomponent.Dimensions
	gasPrice feecomponent.GasPrice

	amountsToBurn  map[ids.ID]uint64
	amountsToStake map[ids.ID]uint64
	complexity     feecomponent.Dimensions

	inputs        []*avax.TransferableInput
	changeOutputs []*avax.TransferableOutput
	stakeOutputs  []*avax.TransferableOutput
}

func (s *spendHelper) addInput(input *avax.TransferableInput) error {
	newInputComplexity, err := txfee.InputComplexity(input)
	if err != nil {
		return err
	}
	s.complexity, err = s.complexity.Add(&newInputComplexity)
	if err != nil {
		return err
	}

	s.inputs = append(s.inputs, input)
	return nil
}

func (s *spendHelper) addChangeOutput(output *avax.TransferableOutput) error {
	s.changeOutputs = append(s.changeOutputs, output)
	return s.addOutputComplexity(output)
}

func (s *spendHelper) addStakedOutput(output *avax.TransferableOutput) error {
	s.stakeOutputs = append(s.stakeOutputs, output)
	return s.addOutputComplexity(output)
}

func (s *spendHelper) addOutputComplexity(output *avax.TransferableOutput) error {
	newOutputComplexity, err := txfee.OutputComplexity(output)
	if err != nil {
		return err
	}
	s.complexity, err = s.complexity.Add(&newOutputComplexity)
	return err
}

func (s *spendHelper) shouldConsumeLockedAsset(assetID ids.ID) bool {
	remainingAmountToStake := s.amountsToStake[assetID]
	return remainingAmountToStake != 0
}

func (s *spendHelper) shouldConsumeAsset(assetID ids.ID) bool {
	remainingAmountToBurn := s.amountsToBurn[assetID]
	remainingAmountToStake := s.amountsToStake[assetID]
	return remainingAmountToBurn != 0 || remainingAmountToStake != 0
}

func (s *spendHelper) consumeLockedAsset(assetID ids.ID, amount uint64) uint64 {
	remainingAmountToStake := s.amountsToStake[assetID]
	// Stake any value that should be staked
	amountToStake := min(
		remainingAmountToStake, // Amount we still need to stake
		amount,                 // Amount available to stake
	)
	s.amountsToStake[assetID] -= amountToStake
	return amount - amountToStake
}

func (s *spendHelper) consumeAsset(assetID ids.ID, amount uint64) uint64 {
	remainingAmountToBurn := s.amountsToBurn[assetID]

	// Burn any value that should be burned
	amountToBurn := min(
		remainingAmountToBurn, // Amount we still need to burn
		amount,                // Amount available to burn
	)
	s.amountsToBurn[assetID] -= amountToBurn

	// Stake any remaining value that should be staked
	return s.consumeLockedAsset(assetID, amount-amountToBurn)
}

func (s *spendHelper) calculateFee() (uint64, error) {
	gas, err := s.complexity.ToGas(s.weights)
	if err != nil {
		return 0, err
	}
	return gas.Cost(s.gasPrice)
}

func (s *spendHelper) verifyAssetsConsumed() error {
	for assetID, amount := range s.amountsToStake {
		if amount != 0 {
			return fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q to stake",
				ErrInsufficientFunds,
				amount,
				assetID,
			)
		}
	}
	for assetID, amount := range s.amountsToBurn {
		if amount != 0 {
			return fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q",
				ErrInsufficientFunds,
				amount,
				assetID,
			)
		}
	}
	return nil
}

func splitLockedStakeableUTXOs(utxos []*avax.UTXO, minIssuanceTime uint64) ([]*avax.UTXO, []*avax.UTXO) {
	var (
		lockedUTXOs   = make([]*avax.UTXO, 0, len(utxos))
		unlockedUTXOs = make([]*avax.UTXO, 0, len(utxos))
	)
	for _, utxo := range utxos {
		lockedOut, ok := utxo.Out.(*stakeable.LockOut)
		if !ok {
			unlockedUTXOs = append(unlockedUTXOs, utxo)
			continue
		}
		if minIssuanceTime >= lockedOut.Locktime {
			unlockedUTXOs = append(unlockedUTXOs, utxo)
			continue
		}
		lockedUTXOs = append(lockedUTXOs, utxo)
	}
	return lockedUTXOs, unlockedUTXOs
}

func splitAVAXUTXOs(utxos []*avax.UTXO, avaxAssetID ids.ID) ([]*avax.UTXO, []*avax.UTXO) {
	var (
		avaxUTXOs    = make([]*avax.UTXO, 0, len(utxos))
		nonAVAXUTXOs = make([]*avax.UTXO, 0, len(utxos))
	)
	for _, utxo := range utxos {
		if utxo.AssetID() == avaxAssetID {
			avaxUTXOs = append(avaxUTXOs, utxo)
		} else {
			nonAVAXUTXOs = append(nonAVAXUTXOs, utxo)
		}
	}
	return avaxUTXOs, nonAVAXUTXOs
}

func unwrapOutput(output verify.State) (*secp256k1fx.TransferOutput, uint64, error) {
	var locktime uint64
	if lockedOut, ok := output.(*stakeable.LockOut); ok {
		output = lockedOut.TransferableOut
		locktime = lockedOut.Locktime
	}

	out, ok := output.(*secp256k1fx.TransferOutput)
	if !ok {
		return nil, 0, ErrUnknownOutputType
	}
	return out, locktime, nil
}
