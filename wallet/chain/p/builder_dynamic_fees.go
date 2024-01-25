// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

type DynamicFeesBuilder struct {
	addrs   set.Set[ids.ShortID]
	backend BuilderBackend
}

func (b *DynamicFeesBuilder) NewBaseTx(
	outputs []*avax.TransferableOutput,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.BaseTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	utx := &txs.BaseTx{
		BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewAddValidatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.AddValidatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)
	utx := &txs.AddValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:        *vdr,
		RewardsOwner:     rewardsOwner,
		DelegationShares: shares,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.AddSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := b.authorizeSubnet(vdr.Subnet, ops)
	if err != nil {
		return nil, err
	}

	utx := &txs.AddSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		SubnetValidator: *vdr,
		SubnetAuth:      subnetAuth,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// update fees to account for the auth credentials to be added upon tx signing
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, subnetAuth.SigIndices)
	if err != nil {
		return nil, fmt.Errorf("failed calculating input size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for input fees: %w", err)
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utx := &txs.RemoveSubnetValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// update fees to account for the auth credentials to be added upon tx signing
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, subnetAuth.SigIndices)
	if err != nil {
		return nil, fmt.Errorf("failed calculating input size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for input fees: %w", err)
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewAddDelegatorTx(
	vdr *txs.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.AddDelegatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)
	utx := &txs.AddDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Validator:              *vdr,
		DelegationRewardsOwner: rewardsOwner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): vdr.Wght,
	}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.CreateChainTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	subnetAuth, err := b.authorizeSubnet(subnetID, ops)
	if err != nil {
		return nil, err
	}

	utils.Sort(fxIDs)

	uTx := &txs.CreateChainTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// update fees to account for the auth credentials to be added upon tx signing
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, subnetAuth.SigIndices)
	if err != nil {
		return nil, fmt.Errorf("failed calculating input size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for input fees: %w", err)
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err = feeCalc.CreateChainTx(uTx); err != nil {
		return nil, err
	}

	inputs, outputs, _, err := b.financeTx(toBurn, toStake, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	uTx.Ins = inputs
	uTx.Outs = outputs

	return uTx, b.initCtx(uTx)
}

func (b *DynamicFeesBuilder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.CreateSubnetTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	utx := &txs.CreateSubnetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		Owner: owner,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toStake := map[ids.ID]uint64{}
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.ExportTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	avax.SortTransferableOutputs(outputs, txs.Codec) // sort exported outputs

	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewTransformSubnetTx(
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
	unitFees, unitCaps commonfees.Dimensions,
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
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// update fees to account for the auth credentials to be added upon tx signing
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, subnetAuth.SigIndices)
	if err != nil {
		return nil, fmt.Errorf("failed calculating input size: %w", err)
	}
	if _, err := feeCalc.AddFeesFor(credsDimensions); err != nil {
		return nil, fmt.Errorf("account for input fees: %w", err)
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewAddPermissionlessValidatorTx(
	vdr *txs.SubnetValidator,
	signer signer.Signer,
	assetID ids.ID,
	validationRewardsOwner *secp256k1fx.OutputOwners,
	delegationRewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.AddPermissionlessValidatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(validationRewardsOwner.Addrs)
	utils.Sort(delegationRewardsOwner.Addrs)

	utx := &txs.AddPermissionlessValidatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) NewAddPermissionlessDelegatorTx(
	vdr *txs.SubnetValidator,
	assetID ids.ID,
	rewardsOwner *secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.AddPermissionlessDelegatorTx, error) {
	ops := common.NewOptions(options)
	utils.Sort(rewardsOwner.Addrs)

	utx := &txs.AddPermissionlessDelegatorTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
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

	feesMan := commonfees.NewManager(unitFees)
	feeCalc := &fees.Calculator{
		IsEForkActive:    true,
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
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

func (b *DynamicFeesBuilder) financeTx(
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

	amountsToBurn[b.backend.AVAXAssetID()] += feeCalc.Fee

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
			return nil, nil, nil, errUnknownOutputType
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

		// update fees to account for the input added
		insDimensions, err := commonfees.GetInputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableInput{input})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed calculating input size: %w", err)
		}
		addedFees, err := feeCalc.AddFeesFor(insDimensions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[b.backend.AVAXAssetID()] += addedFees

		// update fees to account for the credentials to be added with inputs upon tx signing
		credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, inputSigIndices)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed calculating input size: %w", err)
		}
		addedFees, err = feeCalc.AddFeesFor(credsDimensions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[b.backend.AVAXAssetID()] += addedFees

		inputs = append(inputs, input)

		// Stake any value that should be staked
		amountToStake := math.Min(
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

		// update fees to account for the staked output
		outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{stakeOut})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed calculating stakedOut size: %w", err)
		}
		addedFees, err = feeCalc.AddFeesFor(outDimensions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for stakedOut fees: %w", err)
		}
		amountsToBurn[b.backend.AVAXAssetID()] += addedFees

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
			outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{changeOut})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed calculating changeOut size: %w", err)
			}
			addedFees, err := feeCalc.AddFeesFor(outDimensions)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for stakedOut fees: %w", err)
			}
			amountsToBurn[b.backend.AVAXAssetID()] += addedFees

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
			return nil, nil, nil, errUnknownOutputType
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

		// update fees to account for the input added
		insDimensions, err := commonfees.GetInputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableInput{input})
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed calculating input size: %w", err)
		}
		addedFees, err := feeCalc.AddFeesFor(insDimensions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[b.backend.AVAXAssetID()] += addedFees

		// update fees to account for the credentials to be added with inputs upon tx signing
		credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, inputSigIndices)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed calculating input size: %w", err)
		}
		addedFees, err = feeCalc.AddFeesFor(credsDimensions)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[b.backend.AVAXAssetID()] += addedFees

		inputs = append(inputs, input)

		// Burn any value that should be burned
		amountToBurn := math.Min(
			amountsToBurn[assetID], // Amount we still need to burn
			out.Amt,                // Amount available to burn
		)
		amountsToBurn[assetID] -= amountToBurn

		amountAvalibleToStake := out.Amt - amountToBurn
		// Burn any value that should be burned
		amountToStake := math.Min(
			amountsToStake[assetID], // Amount we still need to stake
			amountAvalibleToStake,   // Amount available to stake
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

			// update fees to account for the staked output
			outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{stakeOut})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed calculating stakedOut size: %w", err)
			}
			addedFees, err := feeCalc.AddFeesFor(outDimensions)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for stakedOut fees: %w", err)
			}
			amountsToBurn[b.backend.AVAXAssetID()] += addedFees
		}

		if remainingAmount := amountAvalibleToStake - amountToStake; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned, once fees are removed
			changeOut := &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: *changeOwner,
				},
			}

			// update fees to account for the change output
			outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{changeOut})
			if err != nil {
				return nil, nil, nil, fmt.Errorf("failed calculating changeOut size: %w", err)
			}
			addedFees, err := feeCalc.AddFeesFor(outDimensions)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for stakedOut fees: %w", err)
			}

			switch {
			case addedFees < remainingAmount:
				if assetID == b.backend.AVAXAssetID() {
					changeOut.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmount - addedFees
				} else {
					changeOut.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmount
					amountsToBurn[b.backend.AVAXAssetID()] += addedFees
				}
				changeOutputs = append(changeOutputs, changeOut)
			case addedFees == remainingAmount:
				// fees wholly consume remaining amount. We don't add the change
			case addedFees > remainingAmount:
				amountsToBurn[b.backend.AVAXAssetID()] += addedFees - remainingAmount
			}
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

	utils.Sort(inputs)                                     // sort inputs
	avax.SortTransferableOutputs(changeOutputs, txs.Codec) // sort the change outputs
	avax.SortTransferableOutputs(stakeOutputs, txs.Codec)  // sort stake outputs
	return inputs, changeOutputs, stakeOutputs, nil
}

// TODO ABENEGIA: remove duplication with builder method
func (b *DynamicFeesBuilder) authorizeSubnet(subnetID ids.ID, options *common.Options) (*secp256k1fx.Input, error) {
	subnetTx, err := b.backend.GetTx(options.Context(), subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch subnet %q: %w",
			subnetID,
			err,
		)
	}
	subnet, ok := subnetTx.Unsigned.(*txs.CreateSubnetTx)
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

func (b *DynamicFeesBuilder) initCtx(tx txs.UnsignedTx) error {
	ctx, err := newSnowContext(b.backend)
	if err != nil {
		return err
	}

	tx.InitCtx(ctx)
	return nil
}
