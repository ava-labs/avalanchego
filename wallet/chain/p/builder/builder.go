// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/fx"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fee"
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

	// NewConvertSubnetToL1Tx converts the subnet to a Permissionless L1.
	//
	// - [subnetID] specifies the subnet to be converted
	// - [chainID] specifies which chain the manager is deployed on
	// - [address] specifies the address of the manager
	// - [validators] specifies the initial L1 validators of the L1
	NewConvertSubnetToL1Tx(
		subnetID ids.ID,
		chainID ids.ID,
		address []byte,
		validators []*txs.ConvertSubnetToL1Validator,
		options ...common.Option,
	) (*txs.ConvertSubnetToL1Tx, error)

	// NewRegisterL1ValidatorTx adds a validator to an L1.
	//
	// - [balance] that the validator should allocate to continuous fees
	// - [proofOfPossession] is the BLS PoP for the key included in the Warp
	//   message
	// - [message] is the Warp message that authorizes this validator to be
	//   added
	NewRegisterL1ValidatorTx(
		balance uint64,
		proofOfPossession [bls.SignatureLen]byte,
		message []byte,
		options ...common.Option,
	) (*txs.RegisterL1ValidatorTx, error)

	// NewSetL1ValidatorWeightTx sets the weight of a validator on an L1.
	//
	// - [message] is the Warp message that authorizes this validator's weight
	//   to be changed
	NewSetL1ValidatorWeightTx(
		message []byte,
		options ...common.Option,
	) (*txs.SetL1ValidatorWeightTx, error)

	// NewIncreaseL1ValidatorBalanceTx increases the balance of a validator on
	// an L1 for the continuous fee.
	// the continuous fee.
	//
	// - [validationID] of the validator
	// - [balance] amount to increase the validator's balance by
	NewIncreaseL1ValidatorBalanceTx(
		validationID ids.ID,
		balance uint64,
		options ...common.Option,
	) (*txs.IncreaseL1ValidatorBalanceTx, error)

	// NewDisableL1ValidatorTx disables an L1 validator and returns the
	// remaining funds allocated to the continuous fee to the remaining balance
	// owner.
	//
	// - [validationID] of the validator to disable
	NewDisableL1ValidatorTx(
		validationID ids.ID,
		options ...common.Option,
	) (*txs.DisableL1ValidatorTx, error)

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

	NewAddContinuousValidatorTx(
		vdr *txs.Validator,
		signer signer.Signer,
		assetID ids.ID,
		validationRewardsOwner *secp256k1fx.OutputOwners,
		delegationRewardsOwner *secp256k1fx.OutputOwners,
		configOwner *secp256k1fx.OutputOwners,
		delegationShares uint32,
		autoRestakeShares uint32,
		period time.Duration,
		options ...common.Option,
	) (*txs.AddContinuousValidatorTx, error)

	NewSetAutoRestakeConfigTx(
		txID ids.ID,
		autoRestakeShares *uint32,
		period *uint64,
		options ...common.Option,
	) (*txs.SetAutoRestakeConfigTx, error)
}

type Backend interface {
	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetOwner(ctx context.Context, ownerID ids.ID) (fx.Owner, error)
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
	toBurn := map[ids.ID]uint64{}
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
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	outputComplexity, err := fee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicBaseTxComplexities.Add(
		&memoComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, changeOutputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		avaxAssetID: vdr.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		gas.Dimensions{},
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorize(vdr.Subnet, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := fee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicAddSubnetValidatorTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorize(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := fee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicRemoveSubnetValidatorTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		avaxAssetID: vdr.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		gas.Dimensions{},
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorize(subnetID, ops)
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
	dynamicComplexity := gas.Dimensions{
		gas.Bandwidth: bandwidth,
	}
	authComplexity, err := fee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicCreateChainTxComplexities.Add(
		&dynamicComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	ownerComplexity, err := fee.OwnerComplexity(owner)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicCreateSubnetTxComplexities.Add(
		&memoComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorize(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := fee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	ownerComplexity, err := fee.OwnerComplexity(owner)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicTransferSubnetOwnershipTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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

func (b *builder) NewConvertSubnetToL1Tx(
	subnetID ids.ID,
	chainID ids.ID,
	address []byte,
	validators []*txs.ConvertSubnetToL1Validator,
	options ...common.Option,
) (*txs.ConvertSubnetToL1Tx, error) {
	var avaxToBurn uint64
	for _, vdr := range validators {
		var err error
		avaxToBurn, err = math.Add(avaxToBurn, vdr.Balance)
		if err != nil {
			return nil, err
		}
	}

	var (
		toBurn = map[ids.ID]uint64{
			b.context.AVAXAssetID: avaxToBurn,
		}
		toStake = map[ids.ID]uint64{}
		ops     = common.NewOptions(options)
	)
	subnetAuth, err := b.authorize(subnetID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	additionalBytes, err := math.Add(uint64(len(memo)), uint64(len(address)))
	if err != nil {
		return nil, err
	}
	bytesComplexity := gas.Dimensions{
		gas.Bandwidth: additionalBytes,
	}
	validatorComplexity, err := fee.ConvertSubnetToL1ValidatorComplexity(validators...)
	if err != nil {
		return nil, err
	}
	authComplexity, err := fee.AuthComplexity(subnetAuth)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicConvertSubnetToL1TxComplexities.Add(
		&bytesComplexity,
		&validatorComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	utils.Sort(validators)
	tx := &txs.ConvertSubnetToL1Tx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		Subnet:     subnetID,
		ChainID:    chainID,
		Address:    address,
		Validators: validators,
		SubnetAuth: subnetAuth,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewRegisterL1ValidatorTx(
	balance uint64,
	proofOfPossession [bls.SignatureLen]byte,
	message []byte,
	options ...common.Option,
) (*txs.RegisterL1ValidatorTx, error) {
	var (
		toBurn = map[ids.ID]uint64{
			b.context.AVAXAssetID: balance,
		}
		toStake = map[ids.ID]uint64{}

		ops            = common.NewOptions(options)
		memo           = ops.Memo()
		memoComplexity = gas.Dimensions{
			gas.Bandwidth: uint64(len(memo)),
		}
	)
	warpComplexity, err := fee.WarpComplexity(message)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicRegisterL1ValidatorTxComplexities.Add(
		&memoComplexity,
		&warpComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	tx := &txs.RegisterL1ValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		Balance:           balance,
		ProofOfPossession: proofOfPossession,
		Message:           message,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewSetL1ValidatorWeightTx(
	message []byte,
	options ...common.Option,
) (*txs.SetL1ValidatorWeightTx, error) {
	var (
		toBurn         = map[ids.ID]uint64{}
		toStake        = map[ids.ID]uint64{}
		ops            = common.NewOptions(options)
		memo           = ops.Memo()
		memoComplexity = gas.Dimensions{
			gas.Bandwidth: uint64(len(memo)),
		}
	)
	warpComplexity, err := fee.WarpComplexity(message)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicSetL1ValidatorWeightTxComplexities.Add(
		&memoComplexity,
		&warpComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	tx := &txs.SetL1ValidatorWeightTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		Message: message,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewIncreaseL1ValidatorBalanceTx(
	validationID ids.ID,
	balance uint64,
	options ...common.Option,
) (*txs.IncreaseL1ValidatorBalanceTx, error) {
	var (
		toBurn = map[ids.ID]uint64{
			b.context.AVAXAssetID: balance,
		}
		toStake        = map[ids.ID]uint64{}
		ops            = common.NewOptions(options)
		memo           = ops.Memo()
		memoComplexity = gas.Dimensions{
			gas.Bandwidth: uint64(len(memo)),
		}
	)
	complexity, err := fee.IntrinsicIncreaseL1ValidatorBalanceTxComplexities.Add(
		&memoComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	tx := &txs.IncreaseL1ValidatorBalanceTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		ValidationID: validationID,
		Balance:      balance,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewDisableL1ValidatorTx(
	validationID ids.ID,
	options ...common.Option,
) (*txs.DisableL1ValidatorTx, error) {
	var (
		toBurn  = map[ids.ID]uint64{}
		toStake = map[ids.ID]uint64{}
		ops     = common.NewOptions(options)
	)
	disableAuth, err := b.authorize(validationID, ops)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	authComplexity, err := fee.AuthComplexity(disableAuth)
	if err != nil {
		return nil, err
	}

	complexity, err := fee.IntrinsicDisableL1ValidatorTxComplexities.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	tx := &txs.DisableL1ValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		ValidationID: validationID,
		DisableAuth:  disableAuth,
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
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	inputComplexity, err := fee.InputComplexity(importedInputs...)
	if err != nil {
		return nil, err
	}
	outputComplexity, err := fee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicImportTxComplexities.Add(
		&memoComplexity,
		&inputComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	var (
		toBurn  = map[ids.ID]uint64{}
		toStake = map[ids.ID]uint64{}
	)
	excessAVAX := importedAmounts[avaxAssetID]

	inputs, changeOutputs, _, err := b.spend(
		toBurn,
		toStake,
		excessAVAX,
		complexity,
		to,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
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
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	outputComplexity, err := fee.OutputComplexity(outputs...)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicExportTxComplexities.Add(
		&memoComplexity,
		&outputComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, changeOutputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{
		assetID: maxSupply - initialSupply,
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	subnetAuth, err := b.authorize(subnetID, ops)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		gas.Dimensions{},
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	signerComplexity, err := fee.SignerComplexity(signer)
	if err != nil {
		return nil, err
	}
	validatorOwnerComplexity, err := fee.OwnerComplexity(validationRewardsOwner)
	if err != nil {
		return nil, err
	}
	delegatorOwnerComplexity, err := fee.OwnerComplexity(delegationRewardsOwner)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicAddPermissionlessValidatorTxComplexities.Add(
		&memoComplexity,
		&signerComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	ownerComplexity, err := fee.OwnerComplexity(rewardsOwner)
	if err != nil {
		return nil, err
	}
	complexity, err := fee.IntrinsicAddPermissionlessDelegatorTxComplexities.Add(
		&memoComplexity,
		&ownerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
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

func (b *builder) NewAddContinuousValidatorTx(
	vdr *txs.Validator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	configOwner *secp256k1fx.OutputOwners,
	delegationShares uint32,
	autoRestakeShares uint32,
	period time.Duration,
	options ...common.Option,
) (*txs.AddContinuousValidatorTx, error) {
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		assetID: vdr.Wght,
	}

	ops := common.NewOptions(options)
	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}
	signerComplexity, err := fee.SignerComplexity(signer)
	if err != nil {
		return nil, err
	}
	validatorOwnerComplexity, err := fee.OwnerComplexity(validationRewardsOwner)
	if err != nil {
		return nil, err
	}
	delegatorOwnerComplexity, err := fee.OwnerComplexity(delegationRewardsOwner)
	if err != nil {
		return nil, err
	}
	configOwnerComplexity, err := fee.OwnerComplexity(configOwner)
	if err != nil {
		return nil, err
	}

	complexity, err := fee.IntrinsicAddContinuousValidatorTxComplexities.Add(
		&memoComplexity,
		&signerComplexity,
		&validatorOwnerComplexity,
		&delegatorOwnerComplexity,
		&configOwnerComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, baseOutputs, stakeOutputs, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	utils.Sort(validationRewardsOwner.Addrs)
	utils.Sort(delegationRewardsOwner.Addrs)
	tx := &txs.AddContinuousValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         memo,
		}},
		ValidatorNodeID:       vdr.NodeID,
		Signer:                signer,
		StakeOuts:             stakeOutputs,
		ValidatorRewardsOwner: validationRewardsOwner,
		DelegatorRewardsOwner: delegationRewardsOwner,
		ConfigOwner:           configOwner,
		DelegationShares:      delegationShares,
		Wght:                  vdr.Wght,
		AutoRestakeShares:     autoRestakeShares,
		Period:                uint64(period.Seconds()),
	}

	return tx, b.initCtx(tx)
}

func (b *builder) NewSetAutoRestakeConfigTx(
	txID ids.ID,
	autoRestakeShares *uint32,
	period *uint64,
	options ...common.Option,
) (*txs.SetAutoRestakeConfigTx, error) {
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)

	auth, err := b.authorize(txID, ops)
	if err != nil {
		return nil, err
	}

	authComplexity, err := fee.AuthComplexity(auth)
	if err != nil {
		return nil, err
	}

	memo := ops.Memo()
	memoComplexity := gas.Dimensions{
		gas.Bandwidth: uint64(len(memo)),
	}

	complexity, err := fee.IntrinsicSetAutoRestakeConfigTx.Add(
		&memoComplexity,
		&authComplexity,
	)
	if err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.spend(
		toBurn,
		toStake,
		0,
		complexity,
		nil,
		ops,
	)
	if err != nil {
		return nil, err
	}

	var autoRestakeSharesVal uint32
	if autoRestakeShares != nil {
		autoRestakeSharesVal = *autoRestakeShares
	}

	var periodVal uint64
	if period != nil {
		periodVal = *period
	}

	tx := &txs.SetAutoRestakeConfigTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         memo,
		}},
		TxID:                 txID,
		Auth:                 auth,
		AutoRestakeShares:    autoRestakeSharesVal,
		HasAutoRestakeShares: autoRestakeShares != nil,
		Period:               periodVal,
		HasPeriod:            period != nil,
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
//   - [toBurn] maps assetID to the amount of the asset to spend without
//     producing an output. This is typically used for fees. However, it can
//     also be used to consume some of an asset that will be produced in
//     separate outputs, such as ExportedOutputs. Only unlocked UTXOs are able
//     to be burned here.
//   - [toStake] maps assetID to the amount of the asset to spend and place into
//     the staked outputs. First locked UTXOs are attempted to be used for these
//     funds, and then unlocked UTXOs will be attempted to be used. There is no
//     preferential ordering on the unlock times.
//   - [excessAVAX] contains the amount of extra AVAX that spend can produce in
//     the change outputs in addition to the consumed and not burned AVAX.
//   - [complexity] contains the currently accrued transaction complexity that
//     will be used to calculate the required fees to be burned.
//   - [ownerOverride] optionally specifies the output owners to use for the
//     unlocked AVAX change output if no additional AVAX was needed to be
//     burned. If this value is nil, the default change owner is used.
func (b *builder) spend(
	toBurn map[ids.ID]uint64,
	toStake map[ids.ID]uint64,
	excessAVAX uint64,
	complexity gas.Dimensions,
	ownerOverride *secp256k1fx.OutputOwners,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	changeOutputs []*avax.TransferableOutput,
	stakeOutputs []*avax.TransferableOutput,
	err error,
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

		toBurn:     toBurn,
		toStake:    toStake,
		complexity: complexity,

		// Initialize the return values with empty slices to preserve backward
		// compatibility of the json representation of transactions with no
		// inputs or outputs.
		inputs:        []*avax.TransferableInput{},
		changeOutputs: []*avax.TransferableOutput{},
		stakeOutputs:  []*avax.TransferableOutput{},
	}

	utxosByLocktime := splitByLocktime(utxos, minIssuanceTime)
	for _, utxo := range utxosByLocktime.locked {
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
	for assetID, amount := range s.toStake {
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
	utxosByAVAXAssetID := splitByAssetID(utxosByLocktime.unlocked, b.context.AVAXAssetID)
	for _, utxo := range utxosByAVAXAssetID.other {
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

	for _, utxo := range utxosByAVAXAssetID.requested {
		requiredFee, err := s.calculateFee()
		if err != nil {
			return nil, nil, nil, err
		}

		// If we don't need to burn or stake additional AVAX and we have
		// consumed enough AVAX to pay the required fee, we should stop
		// consuming UTXOs.
		if !s.shouldConsumeAsset(b.context.AVAXAssetID) && excessAVAX >= requiredFee {
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
		excessAVAX, err = math.Add(excessAVAX, excess)
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
	if excessAVAX < requiredFee {
		return nil, nil, nil, fmt.Errorf(
			"%w: provided UTXOs needed %d more nAVAX (%q)",
			ErrInsufficientFunds,
			requiredFee-excessAVAX,
			b.context.AVAXAssetID,
		)
	}

	secpExcessAVAXOutput := &secp256k1fx.TransferOutput{
		Amt:          0, // Populated later if used
		OutputOwners: *ownerOverride,
	}
	excessAVAXOutput := &avax.TransferableOutput{
		Asset: avax.Asset{
			ID: b.context.AVAXAssetID,
		},
		Out: secpExcessAVAXOutput,
	}
	if err := s.addOutputComplexity(excessAVAXOutput); err != nil {
		return nil, nil, nil, err
	}

	requiredFeeWithChange, err := s.calculateFee()
	if err != nil {
		return nil, nil, nil, err
	}
	if excessAVAX > requiredFeeWithChange {
		// It is worth adding the change output
		secpExcessAVAXOutput.Amt = excessAVAX - requiredFeeWithChange
		s.changeOutputs = append(s.changeOutputs, excessAVAXOutput)
	}

	utils.Sort(s.inputs)                                     // sort inputs
	avax.SortTransferableOutputs(s.changeOutputs, txs.Codec) // sort the change outputs
	avax.SortTransferableOutputs(s.stakeOutputs, txs.Codec)  // sort stake outputs
	return s.inputs, s.changeOutputs, s.stakeOutputs, nil
}

func (b *builder) authorize(ownerID ids.ID, options *common.Options) (*secp256k1fx.Input, error) {
	ownerIntf, err := b.backend.GetOwner(options.Context(), ownerID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch owner for %q: %w",
			ownerID,
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
	weights  gas.Dimensions
	gasPrice gas.Price

	toBurn     map[ids.ID]uint64
	toStake    map[ids.ID]uint64
	complexity gas.Dimensions

	inputs        []*avax.TransferableInput
	changeOutputs []*avax.TransferableOutput
	stakeOutputs  []*avax.TransferableOutput
}

func (s *spendHelper) addInput(input *avax.TransferableInput) error {
	newInputComplexity, err := fee.InputComplexity(input)
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
	newOutputComplexity, err := fee.OutputComplexity(output)
	if err != nil {
		return err
	}
	s.complexity, err = s.complexity.Add(&newOutputComplexity)
	return err
}

func (s *spendHelper) shouldConsumeLockedAsset(assetID ids.ID) bool {
	return s.toStake[assetID] != 0
}

func (s *spendHelper) shouldConsumeAsset(assetID ids.ID) bool {
	return s.toBurn[assetID] != 0 || s.shouldConsumeLockedAsset(assetID)
}

func (s *spendHelper) consumeLockedAsset(assetID ids.ID, amount uint64) uint64 {
	// Stake any value that should be staked
	toStake := min(
		s.toStake[assetID], // Amount we still need to stake
		amount,             // Amount available to stake
	)
	s.toStake[assetID] -= toStake
	return amount - toStake
}

func (s *spendHelper) consumeAsset(assetID ids.ID, amount uint64) uint64 {
	// Burn any value that should be burned
	toBurn := min(
		s.toBurn[assetID], // Amount we still need to burn
		amount,            // Amount available to burn
	)
	s.toBurn[assetID] -= toBurn

	// Stake any remaining value that should be staked
	return s.consumeLockedAsset(assetID, amount-toBurn)
}

func (s *spendHelper) calculateFee() (uint64, error) {
	gas, err := s.complexity.ToGas(s.weights)
	if err != nil {
		return 0, err
	}
	return gas.Cost(s.gasPrice)
}

func (s *spendHelper) verifyAssetsConsumed() error {
	for assetID, amount := range s.toStake {
		if amount == 0 {
			continue
		}

		return fmt.Errorf(
			"%w: provided UTXOs need %d more units of asset %q to stake",
			ErrInsufficientFunds,
			amount,
			assetID,
		)
	}
	for assetID, amount := range s.toBurn {
		if amount == 0 {
			continue
		}

		return fmt.Errorf(
			"%w: provided UTXOs need %d more units of asset %q",
			ErrInsufficientFunds,
			amount,
			assetID,
		)
	}
	return nil
}

type utxosByLocktime struct {
	unlocked []*avax.UTXO
	locked   []*avax.UTXO
}

// splitByLocktime separates the provided UTXOs into two slices:
// 1. UTXOs that are unlocked with the provided issuance time
// 2. UTXOs that are locked with the provided issuance time
func splitByLocktime(utxos []*avax.UTXO, minIssuanceTime uint64) utxosByLocktime {
	split := utxosByLocktime{
		unlocked: make([]*avax.UTXO, 0, len(utxos)),
		locked:   make([]*avax.UTXO, 0, len(utxos)),
	}
	for _, utxo := range utxos {
		if lockedOut, ok := utxo.Out.(*stakeable.LockOut); ok && minIssuanceTime < lockedOut.Locktime {
			split.locked = append(split.locked, utxo)
		} else {
			split.unlocked = append(split.unlocked, utxo)
		}
	}
	return split
}

type utxosByAssetID struct {
	requested []*avax.UTXO
	other     []*avax.UTXO
}

// splitByAssetID separates the provided UTXOs into two slices:
// 1. UTXOs with the provided assetID
// 2. UTXOs with a different assetID
func splitByAssetID(utxos []*avax.UTXO, assetID ids.ID) utxosByAssetID {
	split := utxosByAssetID{
		requested: make([]*avax.UTXO, 0, len(utxos)),
		other:     make([]*avax.UTXO, 0, len(utxos)),
	}
	for _, utxo := range utxos {
		if utxo.AssetID() == assetID {
			split.requested = append(split.requested, utxo)
		} else {
			split.other = append(split.other, utxo)
		}
	}
	return split
}

// unwrapOutput returns the *secp256k1fx.TransferOutput that was, potentially,
// wrapped by a *stakeable.LockOut.
//
// If the output was stakeable and locked, the locktime is returned. Otherwise,
// the locktime returned will be 0.
//
// If the output is not a, potentially wrapped, *secp256k1fx.TransferOutput, an
// error is returned.
func unwrapOutput(output verify.State) (*secp256k1fx.TransferOutput, uint64, error) {
	var locktime uint64
	if lockedOut, ok := output.(*stakeable.LockOut); ok {
		output = lockedOut.TransferableOut
		locktime = lockedOut.Locktime
	}

	unwrappedOutput, ok := output.(*secp256k1fx.TransferOutput)
	if !ok {
		return nil, 0, ErrUnknownOutputType
	}
	return unwrappedOutput, locktime, nil
}
