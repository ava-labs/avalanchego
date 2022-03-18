// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"errors"
	"fmt"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var (
	errNoChangeAddress           = errors.New("no possible change address")
	errWrongTxType               = errors.New("wrong tx type")
	errUnknownOwnerType          = errors.New("unknown owner type")
	errInsufficientAuthorization = errors.New("insufficient authorization")
	errInsufficientFunds         = errors.New("insufficient funds")

	_ Builder = &builder{}
)

// Builder provides a convenient interface for building unsigned P-chain
// transactions.
type Builder interface {
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

	// NewBaseTx creates a new simple value transfer. Because the P-chain
	// doesn't intend for balance transfers to occur, this method is expensive
	// and abuses the creation of subnets.
	//
	// - [outputs] specifies all the recipients and amounts that should be sent
	//   from this transaction.
	NewBaseTx(
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*platformvm.UnsignedCreateSubnetTx, error)

	// NewAddValidatorTx creates a new validator of the primary network.
	//
	// - [validator] specifies all the details of the validation period such as
	//   the startTime, endTime, stake weight, and nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this validator
	//   may accrue during its validation period.
	// - [shares] specifies the fraction (out of 1,000,000) that this validator
	//   will take from delegation rewards. If 1,000,000 is provided, 100% of
	//   the delegation reward will be sent to the validator's [rewardsOwner].
	NewAddValidatorTx(
		validator *platformvm.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		options ...common.Option,
	) (*platformvm.UnsignedAddValidatorTx, error)

	// NewAddSubnetValidatorTx creates a new validator of a subnet.
	//
	// - [validator] specifies all the details of the validation period such as
	//   the startTime, endTime, sampling weight, nodeID, and subnetID.
	NewAddSubnetValidatorTx(
		validator *platformvm.SubnetValidator,
		options ...common.Option,
	) (*platformvm.UnsignedAddSubnetValidatorTx, error)

	// NewAddDelegatorTx creates a new delegator to a validator on the primary
	// network.
	//
	// - [validator] specifies all the details of the delegation period such as
	//   the startTime, endTime, stake weight, and validator's nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this delegator
	//   may accrue at the end of its delegation period.
	NewAddDelegatorTx(
		validator *platformvm.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*platformvm.UnsignedAddDelegatorTx, error)

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
	) (*platformvm.UnsignedCreateChainTx, error)

	// NewCreateSubnetTx creates a new subnet with the specified owner.
	//
	// - [owner] specifies who has the ability to create new chains and add new
	//   validators to the subnet.
	NewCreateSubnetTx(
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*platformvm.UnsignedCreateSubnetTx, error)

	// NewImportTx creates an import transaction that attempts to consume all
	// the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	NewImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*platformvm.UnsignedImportTx, error)

	// NewExportTx creates an export transaction that attempts to send all the
	// provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	NewExportTx(
		chainID ids.ID,
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*platformvm.UnsignedExportTx, error)
}

// BuilderBackend specifies the required information needed to build unsigned
// P-chain transactions.
type BuilderBackend interface {
	Context
	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	GetTx(ctx stdcontext.Context, txID ids.ID) (*platformvm.Tx, error)
}

type builder struct {
	addrs   ids.ShortSet
	backend BuilderBackend
}

// NewBuilder returns a new transaction builder.
//
// - [addrs] is the set of addresses that the builder assumes can be used when
//   signing the transactions in the future.
// - [backend] provides the required access to the chain's context and state to
//   build out the transactions.
func NewBuilder(addrs ids.ShortSet, backend BuilderBackend) Builder {
	return &builder{
		addrs:   addrs,
		backend: backend,
	}
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
) (*platformvm.UnsignedCreateSubnetTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateSubnetTxFee(),
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}
	toStake := map[ids.ID]uint64{}

	ops := common.NewOptions(options)
	inputs, changeOutputs, _, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}
	outputs = append(outputs, changeOutputs...)
	avax.SortTransferableOutputs(outputs, platformvm.Codec) // sort the outputs

	return &platformvm.UnsignedCreateSubnetTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Owner: &secp256k1fx.OutputOwners{},
	}, nil
}

func (b *builder) NewAddValidatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (*platformvm.UnsignedAddValidatorTx, error) {
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): validator.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	ids.SortShortIDs(rewardsOwner.Addrs)
	return &platformvm.UnsignedAddValidatorTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         ops.Memo(),
		}},
		Validator:    *validator,
		Stake:        stakeOutputs,
		RewardsOwner: rewardsOwner,
		Shares:       shares,
	}, nil
}

func (b *builder) NewAddSubnetValidatorTx(
	validator *platformvm.SubnetValidator,
	options ...common.Option,
) (*platformvm.UnsignedAddSubnetValidatorTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateSubnetTxFee(),
	}
	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)
	inputs, outputs, _, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	subnetAuth, err := b.authorizeSubnet(validator.Subnet, ops)
	if err != nil {
		return nil, err
	}

	return &platformvm.UnsignedAddSubnetValidatorTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Validator:  *validator,
		SubnetAuth: subnetAuth,
	}, nil
}

func (b *builder) NewAddDelegatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*platformvm.UnsignedAddDelegatorTx, error) {
	toBurn := map[ids.ID]uint64{}
	toStake := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): validator.Wght,
	}
	ops := common.NewOptions(options)
	inputs, baseOutputs, stakeOutputs, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	ids.SortShortIDs(rewardsOwner.Addrs)
	return &platformvm.UnsignedAddDelegatorTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         baseOutputs,
			Memo:         ops.Memo(),
		}},
		Validator:    *validator,
		Stake:        stakeOutputs,
		RewardsOwner: rewardsOwner,
	}, nil
}

func (b *builder) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (*platformvm.UnsignedCreateChainTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateSubnetTxFee(),
	}
	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)
	inputs, outputs, _, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	ids.SortIDs(fxIDs)
	return &platformvm.UnsignedCreateChainTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		SubnetID:    subnetID,
		ChainName:   chainName,
		VMID:        vmID,
		FxIDs:       fxIDs,
		GenesisData: genesis,
		SubnetAuth:  subnetAuth,
	}, nil
}

func (b *builder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*platformvm.UnsignedCreateSubnetTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateSubnetTxFee(),
	}
	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)
	inputs, outputs, _, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	ids.SortShortIDs(owner.Addrs)
	return &platformvm.UnsignedCreateSubnetTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Owner: owner,
	}, nil
}

func (b *builder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*platformvm.UnsignedImportTx, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), sourceChainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.backend.AVAXAssetID()
		txFee           = b.backend.BaseTxFee()

		importedInputs = make([]*avax.TransferableInput, 0, len(utxos))
		importedAmount uint64
	)
	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
		if utxo.AssetID() != avaxAssetID {
			// Currently - only AVAX is allowed to be imported to the P-chain
			continue
		}

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
		newImportedAmount, err := math.Add64(importedAmount, out.Amt)
		if err != nil {
			return nil, err
		}
		importedAmount = newImportedAmount
	}
	avax.SortTransferableInputs(importedInputs) // sort imported inputs

	if len(importedInputs) == 0 {
		return nil, fmt.Errorf(
			"%w: no UTXOs available to import",
			errInsufficientFunds,
		)
	}

	var (
		inputs  []*avax.TransferableInput
		outputs []*avax.TransferableOutput
	)
	if importedAmount < txFee { // imported amount goes toward paying tx fee
		toBurn := map[ids.ID]uint64{
			avaxAssetID: txFee - importedAmount,
		}
		toStake := map[ids.ID]uint64{}
		var err error
		inputs, outputs, _, err = b.spend(toBurn, toStake, ops)
		if err != nil {
			return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
		}
	} else if importedAmount > txFee {
		outputs = append(outputs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt:          importedAmount - txFee,
				OutputOwners: *to,
			},
		})
	}

	return &platformvm.UnsignedImportTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		SourceChain:    sourceChainID,
		ImportedInputs: importedInputs,
	}, nil
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*platformvm.UnsignedExportTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.BaseTxFee(),
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	toStake := map[ids.ID]uint64{}
	ops := common.NewOptions(options)
	inputs, changeOutputs, _, err := b.spend(toBurn, toStake, ops)
	if err != nil {
		return nil, err
	}

	avax.SortTransferableOutputs(outputs, platformvm.Codec) // sort exported outputs
	return &platformvm.UnsignedExportTx{
		BaseTx: platformvm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         changeOutputs,
			Memo:         ops.Memo(),
		}},
		DestinationChain: chainID,
		ExportedOutputs:  outputs,
	}, nil
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
		if lockedOut, ok := outIntf.(*platformvm.StakeableLockOut); ok {
			if !options.AllowStakeableLocked() && lockedOut.Locktime > minIssuanceTime {
				// This output is currently locked, so this output can't be
				// burned.
				continue
			}
			outIntf = lockedOut.TransferableOut
		}

		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errUnknownOutputType
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
// - [amountsToBurn] maps assetID to the amount of the asset to spend without
//   producing an output. This is typically used for fees. However, it can also
//   be used to consume some of an asset that will be produced in separate
//   outputs, such as ExportedOutputs. Only unlocked UTXOs are able to be
//   burned here.
// - [amountsToStake] maps assetID to the amount of the asset to spend and place
//   into the staked outputs. First locked UTXOs are attempted to be used for
//   these funds, and then unlocked UTXOs will be attempted to be used. There is
//   no preferential ordering on the unlock times.
func (b *builder) spend(
	amountsToBurn map[ids.ID]uint64,
	amountsToStake map[ids.ID]uint64,
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
		return nil, nil, nil, errNoChangeAddress
	}
	changeOwner := options.ChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	})

	// Iterate over the locked UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		remainingAmountToStake := amountsToStake[assetID]

		// If we have staked enough of the asset, then we have no need burn
		// more.
		if remainingAmountToStake == 0 {
			continue
		}

		outIntf := utxo.Out
		lockedOut, ok := outIntf.(*platformvm.StakeableLockOut)
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
			return nil, nil, nil, errUnknownOutputType
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		inputs = append(inputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &platformvm.StakeableLockIn{
				Locktime: lockedOut.Locktime,
				TransferableIn: &secp256k1fx.TransferInput{
					Amt: out.Amt,
					Input: secp256k1fx.Input{
						SigIndices: inputSigIndices,
					},
				},
			},
		})

		// Stake any value that should be staked
		amountToStake := math.Min64(
			remainingAmountToStake, // Amount we still need to stake
			out.Amt,                // Amount available to stake
		)

		// Add the output to the staked outputs
		stakeOutputs = append(stakeOutputs, &avax.TransferableOutput{
			Asset: utxo.Asset,
			Out: &platformvm.StakeableLockOut{
				Locktime: lockedOut.Locktime,
				TransferableOut: &secp256k1fx.TransferOutput{
					Amt:          amountToStake,
					OutputOwners: out.OutputOwners,
				},
			},
		})

		amountsToStake[assetID] -= amountToStake
		if remainingAmount := out.Amt - amountToStake; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned
			changeOutputs = append(changeOutputs, &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &platformvm.StakeableLockOut{
					Locktime: lockedOut.Locktime,
					TransferableOut: &secp256k1fx.TransferOutput{
						Amt:          remainingAmount,
						OutputOwners: out.OutputOwners,
					},
				},
			})
		}
	}

	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		remainingAmountToStake := amountsToStake[assetID]
		remainingAmountToBurn := amountsToBurn[assetID]

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if remainingAmountToStake == 0 && remainingAmountToBurn == 0 {
			continue
		}

		outIntf := utxo.Out
		if lockedOut, ok := outIntf.(*platformvm.StakeableLockOut); ok {
			if lockedOut.Locktime > minIssuanceTime {
				// This output is currently locked, so this output can't be
				// burned.
				continue
			}
			outIntf = lockedOut.TransferableOut
		}

		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, nil, nil, errUnknownOutputType
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		inputs = append(inputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			In: &secp256k1fx.TransferInput{
				Amt: out.Amt,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})

		// Burn any value that should be burned
		amountToBurn := math.Min64(
			remainingAmountToBurn, // Amount we still need to burn
			out.Amt,               // Amount available to burn
		)
		amountsToBurn[assetID] -= amountToBurn

		amountAvalibleToStake := out.Amt - amountToBurn
		// Burn any value that should be burned
		amountToStake := math.Min64(
			remainingAmountToStake, // Amount we still need to stake
			amountAvalibleToStake,  // Amount available to stake
		)
		amountsToStake[assetID] -= amountToStake
		if amountToStake > 0 {
			// Some of this input was put for staking
			stakeOutputs = append(stakeOutputs, &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					Amt:          amountToStake,
					OutputOwners: *changeOwner,
				},
			})
		}
		if remainingAmount := amountAvalibleToStake - amountToStake; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned
			changeOutputs = append(changeOutputs, &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					Amt:          remainingAmount,
					OutputOwners: *changeOwner,
				},
			})
		}
	}

	for assetID, amount := range amountsToStake {
		if amount != 0 {
			return nil, nil, nil, fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q to stake",
				errInsufficientFunds,
				amount,
				assetID,
			)
		}
	}
	for assetID, amount := range amountsToBurn {
		if amount != 0 {
			return nil, nil, nil, fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q",
				errInsufficientFunds,
				amount,
				assetID,
			)
		}
	}

	avax.SortTransferableInputs(inputs)                           // sort inputs
	avax.SortTransferableOutputs(changeOutputs, platformvm.Codec) // sort the change outputs
	avax.SortTransferableOutputs(stakeOutputs, platformvm.Codec)  // sort stake outputs
	return inputs, changeOutputs, stakeOutputs, nil
}

func (b *builder) authorizeSubnet(subnetID ids.ID, options *common.Options) (*secp256k1fx.Input, error) {
	subnetTx, err := b.backend.GetTx(options.Context(), subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch subnet %q: %w",
			subnetID,
			err,
		)
	}
	subnet, ok := subnetTx.UnsignedTx.(*platformvm.UnsignedCreateSubnetTx)
	if !ok {
		return nil, errWrongTxType
	}

	owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errUnknownOwnerType
	}

	addrs := options.Addresses(b.addrs)
	minIssuanceTime := options.MinIssuanceTime()
	inputSigIndices, ok := common.MatchOwners(owner, addrs, minIssuanceTime)
	if !ok {
		// We can't authorize the subnet
		return nil, errInsufficientAuthorization
	}
	return &secp256k1fx.Input{
		SigIndices: inputSigIndices,
	}, nil
}
