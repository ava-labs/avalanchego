// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"fmt"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
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
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	feesMan := commonfees.NewManager(unitFees, commonfees.EmptyWindows)
	feeCalc := &fees.Calculator{
		IsEUpgradeActive: true,
		Codec:            Parser.Codec(),
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.BaseTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, err := b.financeTx(toBurn, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	outputs = append(outputs, changeOuts...)
	avax.SortTransferableOutputs(outputs, Parser.Codec())
	utx.Ins = inputs
	utx.Outs = outputs

	return utx, b.initCtx(utx)
}

func (b *DynamicFeesBuilder) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.CreateAssetTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	codec := Parser.Codec()
	states := make([]*txs.InitialState, 0, len(initialState))
	for fxIndex, outs := range initialState {
		state := &txs.InitialState{
			FxIndex: fxIndex,
			FxID:    fxIndexToID[fxIndex],
			Outs:    outs,
		}
		state.Sort(codec) // sort the outputs
		states = append(states, state)
	}

	utils.Sort(states) // sort the initial states

	utx := &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: b.backend.BlockchainID(),
			Memo:         ops.Memo(),
		}},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		States:       states,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	feesMan := commonfees.NewManager(unitFees, commonfees.EmptyWindows)
	feeCalc := &fees.Calculator{
		IsEUpgradeActive: true,
		Codec:            Parser.Codec(),
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.CreateAssetTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, err := b.financeTx(toBurn, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = changeOuts

	return utx, b.initCtx(utx)
}

func (b *DynamicFeesBuilder) NewOperationTx(
	operations []*txs.Operation,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.OperationTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	codec := Parser.Codec()
	txs.SortOperations(operations, codec)

	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: b.backend.BlockchainID(),
			Memo:         ops.Memo(),
		}},
		Ops: operations,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	feesMan := commonfees.NewManager(unitFees, commonfees.EmptyWindows)
	feeCalc := &fees.Calculator{
		IsEUpgradeActive: true,
		Codec:            Parser.Codec(),
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.OperationTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, err := b.financeTx(toBurn, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = changeOuts

	return utx, b.initCtx(utx)
}

func (b *DynamicFeesBuilder) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintFTs(b.addrs, b.backend, outputs, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(
		operations,
		unitFees,
		unitCaps,
		options...,
	)
}

func (b *DynamicFeesBuilder) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintNFTs(b.addrs, b.backend, assetID, payload, owners, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(
		operations,
		unitFees,
		unitCaps,
		options...,
	)
}

func (b *DynamicFeesBuilder) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintProperty(b.addrs, b.backend, assetID, owner, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(
		operations,
		unitFees,
		unitCaps,
		options...,
	)
}

func (b *DynamicFeesBuilder) NewOperationTxBurnProperty(
	assetID ids.ID,
	unitFees, unitCaps commonfees.Dimensions,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := burnProperty(b.addrs, b.backend, assetID, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(
		operations,
		unitFees,
		unitCaps,
		options...,
	)
}

func (b *DynamicFeesBuilder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	unitFees, unitCaps commonfees.Dimensions,
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
	utx.ImportedIns = importedInputs

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
	feesMan := commonfees.NewManager(unitFees, commonfees.EmptyWindows)
	feeCalc := &fees.Calculator{
		IsEUpgradeActive: true,
		Codec:            Parser.Codec(),
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.ImportTx(utx); err != nil {
		return nil, err
	}

	for _, sigIndices := range importedSigIndices {
		if _, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(sigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}
	}

	switch importedAVAX := importedAmounts[avaxAssetID]; {
	case importedAVAX == feeCalc.Fee:
		// imported inputs match exactly the fees to be paid
		avax.SortTransferableOutputs(utx.Outs, Parser.Codec()) // sort imported outputs
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
		outDimensions, err := commonfees.MeterOutput(Parser.Codec(), txs.CodecVersion, changeOut)
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
			avax.SortTransferableOutputs(utx.Outs, Parser.Codec()) // sort imported outputs
			return utx, b.initCtx(utx)

		case feeCalc.Fee == importedAVAX:
			// imported fees pays exactly the tx cost. We don't include the outputs
			avax.SortTransferableOutputs(utx.Outs, Parser.Codec()) // sort imported outputs
			return utx, b.initCtx(utx)

		default:
			// imported avax are not enough to pay fees
			// Drop the changeOut and finance the tx
			if _, err := feeCalc.RemoveFeesFor(outDimensions); err != nil {
				return nil, fmt.Errorf("failed reverting change output: %w", err)
			}
			feeCalc.Fee -= importedAVAX
		}
	}

	toBurn := map[ids.ID]uint64{}
	inputs, changeOuts, err := b.financeTx(toBurn, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = append(utx.Outs, changeOuts...)
	avax.SortTransferableOutputs(utx.Outs, Parser.Codec()) // sort imported outputs
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
	avax.SortTransferableOutputs(outputs, Parser.Codec()) // sort exported outputs

	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Memo:         ops.Memo(),
		}},
		DestinationChain: chainID,
		ExportedOuts:     outputs,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add64(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	feesMan := commonfees.NewManager(unitFees, commonfees.EmptyWindows)
	feeCalc := &fees.Calculator{
		IsEUpgradeActive: true,
		Codec:            Parser.Codec(),
		FeeManager:       feesMan,
		ConsumedUnitsCap: unitCaps,
	}

	// feesMan cumulates consumed units. Let's init it with utx filled so far
	if err := feeCalc.ExportTx(utx); err != nil {
		return nil, err
	}

	inputs, changeOuts, err := b.financeTx(toBurn, feeCalc, ops)
	if err != nil {
		return nil, err
	}

	utx.Ins = inputs
	utx.Outs = changeOuts

	return utx, b.initCtx(utx)
}

func (b *DynamicFeesBuilder) financeTx(
	amountsToBurn map[ids.ID]uint64,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	changeOutputs []*avax.TransferableOutput,
	err error,
) {
	avaxAssetID := b.backend.AVAXAssetID()
	utxos, err := b.backend.UTXOs(options.Context(), constants.PlatformChainID)
	if err != nil {
		return nil, nil, err
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
		return nil, nil, errNoChangeAddress
	}
	changeOwner := options.ChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{addr},
	})

	amountsToBurn[avaxAssetID] += feeCalc.Fee

	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if amountsToBurn[assetID] == 0 {
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
			return nil, nil, errUnknownOutputType
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

		addedFees, err := fees.FinanceInput(feeCalc, Parser.Codec(), input)
		if err != nil {
			return nil, nil, fmt.Errorf("account for input fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		addedFees, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(inputSigIndices))
		if err != nil {
			return nil, nil, fmt.Errorf("account for credential fees: %w", err)
		}
		amountsToBurn[avaxAssetID] += addedFees

		inputs = append(inputs, input)

		// Burn any value that should be burned
		amountToBurn := min(
			amountsToBurn[assetID], // Amount we still need to burn
			out.Amt,                // Amount available to burn
		)
		amountsToBurn[assetID] -= amountToBurn

		// Burn any value that should be burned
		if remainingAmount := out.Amt - amountToBurn; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned, once fees are removed
			changeOut := &avax.TransferableOutput{
				Asset: utxo.Asset,
				Out: &secp256k1fx.TransferOutput{
					OutputOwners: *changeOwner,
				},
			}

			// update fees to account for the change output
			addedFees, _, err = fees.FinanceOutput(feeCalc, Parser.Codec(), changeOut)
			if err != nil {
				return nil, nil, fmt.Errorf("account for output fees: %w", err)
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

	for assetID, amount := range amountsToBurn {
		if amount != 0 {
			return nil, nil, fmt.Errorf(
				"%w: provided UTXOs need %d more units of asset %q",
				errInsufficientFunds,
				amount,
				assetID,
			)
		}
	}

	utils.Sort(inputs)                                          // sort inputs
	avax.SortTransferableOutputs(changeOutputs, Parser.Codec()) // sort the change outputs
	return inputs, changeOutputs, nil
}

func (b *DynamicFeesBuilder) initCtx(tx txs.UnsignedTx) error {
	ctx, err := newSnowContext(b.backend)
	if err != nil {
		return err
	}

	tx.InitCtx(ctx)
	return nil
}
