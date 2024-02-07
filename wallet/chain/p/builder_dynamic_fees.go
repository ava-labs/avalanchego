// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"fmt"
	"time"

	"golang.org/x/exp/slices"

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

func NewDynamicFeesBuilder(addrs set.Set[ids.ShortID], backend BuilderBackend) *DynamicFeesBuilder {
	return &DynamicFeesBuilder{
		addrs:   addrs,
		backend: backend,
	}
}

func (b *DynamicFeesBuilder) NewBaseTx(
	outputs []*avax.TransferableOutput,
	feeCalc *fees.Calculator,
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewAddSubnetValidatorTx(
	vdr *txs.SubnetValidator,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.AddSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := authorizeSubnet(b.backend, b.addrs, vdr.Subnet, ops)
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

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = financeCredential(feeCalc, subnetAuth.SigIndices); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewRemoveSubnetValidatorTx(
	nodeID ids.NodeID,
	subnetID ids.ID,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.RemoveSubnetValidatorTx, error) {
	ops := common.NewOptions(options)

	subnetAuth, err := authorizeSubnet(b.backend, b.addrs, subnetID, ops)
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

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = financeCredential(feeCalc, subnetAuth.SigIndices); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewCreateChainTx(
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
	subnetAuth, err := authorizeSubnet(b.backend, b.addrs, subnetID, ops)
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

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = financeCredential(feeCalc, subnetAuth.SigIndices); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
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

	return uTx, initCtx(b.backend, uTx)
}

func (b *DynamicFeesBuilder) NewCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.ImportTx, error) {
	ops := common.NewOptions(options)
	// 1. Build core transaction
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		SourceChain: sourceChainID,
	}

	// 2. Add imported inputs first
	utxos, err := b.backend.UTXOs(ops.Context(), sourceChainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.backend.AVAXAssetID()

		importedInputs     = make([]*avax.TransferableInput, 0, len(utxos))
		importedSigIndices = make([][]uint32, 0)
		importedAmounts    = make(map[ids.ID]uint64)
	)

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
			errInsufficientFunds,
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

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.ImportTx(utx); err != nil {
		return nil, err
	}

	for _, sigIndices := range importedSigIndices {
		if _, err = financeCredential(feeCalc, sigIndices); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}
	}

	switch importedAVAX := importedAmounts[avaxAssetID]; {
	case importedAVAX == feeCalc.Fee:
		// imported inputs match exactly the fees to be paid
		avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
		return utx, initCtx(b.backend, utx)

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
		outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{changeOut})
		if err != nil {
			return nil, fmt.Errorf("failed calculating output size: %w", err)
		}
		if _, err := feeCalc.AddFeesFor(outDimensions); err != nil {
			return nil, fmt.Errorf("account for output fees: %w", err)
		}

		switch {
		case feeCalc.Fee < importedAVAX:
			changeOut.Out.(*secp256k1fx.TransferOutput).Amt = importedAVAX - feeCalc.Fee
			utx.Outs = append(utx.Outs, changeOut)
			avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
			return utx, initCtx(b.backend, utx)

		case feeCalc.Fee == importedAVAX:
			// imported fees pays exactly the tx cost. We don't include the outputs
			avax.SortTransferableOutputs(utx.Outs, txs.Codec) // sort imported outputs
			return utx, initCtx(b.backend, utx)

		default:
			// imported avax are not enough to pay fees
			// Drop the changeOut and finance the tx
			if _, err := feeCalc.RemoveFeesFor(outDimensions); err != nil {
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
	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewExportTx(
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

	return utx, initCtx(b.backend, utx)
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
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.TransformSubnetTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)

	subnetAuth, err := authorizeSubnet(b.backend, b.addrs, subnetID, ops)
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

	// update fees to account for the auth credentials to be added upon tx signing
	if _, err = financeCredential(feeCalc, subnetAuth.SigIndices); err != nil {
		return nil, fmt.Errorf("account for credential fees: %w", err)
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewAddPermissionlessValidatorTx(
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

	return utx, initCtx(b.backend, utx)
}

func (b *DynamicFeesBuilder) NewAddPermissionlessDelegatorTx(
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

	return utx, initCtx(b.backend, utx)
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
	avaxAssetID := b.backend.AVAXAssetID()
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
		return nil, nil, nil, errNoChangeAddress
	}
	changeOwner := options.ChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	})

	amountsToBurn[avaxAssetID] += feeCalc.Fee

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

		addedFees, err := financeInput(feeCalc, input)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		addedFees, err = financeCredential(feeCalc, inputSigIndices)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

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

		addedFees, err = financeOutput(feeCalc, stakeOut)
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
			addedFees, err = financeOutput(feeCalc, changeOut)
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

		addedFees, err := financeInput(feeCalc, input)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		addedFees, err = financeCredential(feeCalc, inputSigIndices)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("account for credential fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

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

			addedFees, err = financeOutput(feeCalc, stakeOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
			}
			amountsToBurn[avaxAssetID] += addedFees
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
			addedFees, err = financeOutput(feeCalc, changeOut)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("account for output fees: %w", err)
			}

			if assetID != avaxAssetID {
				changeOut.Out.(*secp256k1fx.TransferOutput).Amt = remainingAmount
				amountsToBurn[avaxAssetID] += addedFees
				changeOutputs = append(changeOutputs, changeOut)
			} else {
				// here assetID == b.backend.AVAXAssetID()
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

func financeInput(feeCalc *fees.Calculator, input *avax.TransferableInput) (uint64, error) {
	insDimensions, err := commonfees.GetInputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableInput{input})
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(insDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}

func financeOutput(feeCalc *fees.Calculator, output *avax.TransferableOutput) (uint64, error) {
	outDimensions, err := commonfees.GetOutputsDimensions(txs.Codec, txs.CodecVersion, []*avax.TransferableOutput{output})
	if err != nil {
		return 0, fmt.Errorf("failed calculating changeOut size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(outDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for stakedOut fees: %w", err)
	}
	return addedFees, nil
}

func financeCredential(feeCalc *fees.Calculator, inputSigIndices []uint32) (uint64, error) {
	credsDimensions, err := commonfees.GetCredentialsDimensions(txs.Codec, txs.CodecVersion, inputSigIndices)
	if err != nil {
		return 0, fmt.Errorf("failed calculating input size: %w", err)
	}
	addedFees, err := feeCalc.AddFeesFor(credsDimensions)
	if err != nil {
		return 0, fmt.Errorf("account for input fees: %w", err)
	}
	return addedFees, nil
}
