// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/fees"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	commonfees "github.com/ava-labs/avalanchego/vms/components/fees"
)

var (
	errNoChangeAddress   = errors.New("no possible change address")
	errInsufficientFunds = errors.New("insufficient funds")

	fxIndexToID = map[uint32]ids.ID{
		SECP256K1FxIndex: secp256k1fx.ID,
		NFTFxIndex:       nftfx.ID,
		PropertyFxIndex:  propertyfx.ID,
	}

	_ Builder = (*builder)(nil)
)

// Builder provides a convenient interface for building unsigned X-chain
// transactions.
type Builder interface {
	// Context returns the configuration of the chain that this builder uses to
	// create transactions.
	Context() *Context

	// GetFTBalance calculates the amount of each fungible asset that this
	// builder has control over.
	GetFTBalance(
		options ...common.Option,
	) (map[ids.ID]uint64, error)

	// GetImportableBalance calculates the amount of each fungible asset that
	// this builder could import from the provided chain.
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

	// NewCreateAssetTx creates a new asset.
	//
	// - [name] specifies a human readable name for this asset.
	// - [symbol] specifies a human readable abbreviation for this asset.
	// - [denomination] specifies how many times the asset can be split. For
	//   example, a denomination of [4] would mean that the smallest unit of the
	//   asset would be 0.001 units.
	// - [initialState] specifies the supported feature extensions for this
	//   asset as well as the initial outputs for the asset.
	NewCreateAssetTx(
		name string,
		symbol string,
		denomination byte,
		initialState map[uint32][]verify.State,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.CreateAssetTx, error)

	// NewOperationTx performs state changes on the UTXO set. These state
	// changes may be more complex than simple value transfers.
	//
	// - [operations] specifies the state changes to perform.
	NewOperationTx(
		operations []*txs.Operation,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxMintFT performs a set of state changes that mint new tokens
	// for the requested assets.
	//
	// - [outputs] maps the assetID to the output that should be created for the
	//   asset.
	NewOperationTxMintFT(
		outputs map[ids.ID]*secp256k1fx.TransferOutput,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxMintNFT performs a state change that mints new NFTs for the
	// requested asset.
	//
	// - [assetID] specifies the asset to mint the NFTs under.
	// - [payload] specifies the payload to provide each new NFT.
	// - [owners] specifies the new owners of each NFT.
	NewOperationTxMintNFT(
		assetID ids.ID,
		payload []byte,
		owners []*secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxMintProperty performs a state change that mints a new
	// property for the requested asset.
	//
	// - [assetID] specifies the asset to mint the property under.
	// - [owner] specifies the new owner of the property.
	NewOperationTxMintProperty(
		assetID ids.ID,
		owner *secp256k1fx.OutputOwners,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxBurnProperty performs state changes that burns all the
	// properties of the requested asset.
	//
	// - [assetID] specifies the asset to burn the property of.
	NewOperationTxBurnProperty(
		assetID ids.ID,
		feeCalc *fees.Calculator,
		options ...common.Option,
	) (*txs.OperationTx, error)

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
}

type Backend interface {
	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
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

func (b *builder) GetFTBalance(
	options ...common.Option,
) (map[ids.ID]uint64, error) {
	ops := common.NewOptions(options)
	return b.getBalance(b.context.BlockchainID, ops)
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
			BlockchainID: b.context.BlockchainID,
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

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
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

func (b *builder) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	feeCalc *fees.Calculator,
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
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Memo:         ops.Memo(),
		}},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		States:       states,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
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

func (b *builder) NewOperationTx(
	operations []*txs.Operation,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	codec := Parser.Codec()
	txs.SortOperations(operations, codec)

	utx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Memo:         ops.Memo(),
		}},
		Ops: operations,
	}

	// 2. Finance the tx by building the utxos (inputs, outputs and stakes)
	toBurn := map[ids.ID]uint64{} // fees are calculated in financeTx

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
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

func (b *builder) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintFTs(b.addrs, b.backend, b.context, outputs, feeCalc, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, feeCalc, options...)
}

func (b *builder) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintNFTs(b.addrs, b.backend, b.context, assetID, payload, owners, feeCalc, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, feeCalc, options...)
}

func (b *builder) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := mintProperty(b.addrs, b.backend, b.context, assetID, owner, feeCalc, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, feeCalc, options...)
}

func (b *builder) NewOperationTxBurnProperty(
	assetID ids.ID,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := burnProperty(b.addrs, b.backend, b.context, assetID, feeCalc, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, feeCalc, options...)
}

func (b *builder) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.ImportTx, error) {
	ops := common.NewOptions(options)
	// 1. Build core transaction
	utx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Memo:         ops.Memo(),
		}},
		SourceChain: chainID,
	}

	// 2. Add imported inputs first
	utxos, err := b.backend.UTXOs(ops.Context(), chainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.context.AVAXAssetID

		importedInputs     = make([]*avax.TransferableInput, 0, len(utxos))
		importedSigIndices = make([][]uint32, 0)
		importedAmounts    = make(map[ids.ID]uint64)
	)
	// Iterate over the unlocked UTXOs
	for _, utxo := range utxos {
		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			// Can't import an unknown transfer output type
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
			FxID:   secp256k1fx.ID,
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
			FxID:  secp256k1fx.ID,
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

		outDimensions, err := commonfees.MeterOutput(Parser.Codec(), txs.CodecVersion, changeOut)
		if err != nil {
			return nil, fmt.Errorf("failed calculating output size: %w", err)
		}
		if _, err := feeCalc.AddFeesFor(outDimensions, feeCalc.TipPercentage); err != nil {
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
			if _, err := feeCalc.RemoveFeesFor(outDimensions, feeCalc.TipPercentage); err != nil {
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

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	feeCalc *fees.Calculator,
	options ...common.Option,
) (*txs.ExportTx, error) {
	// 1. Build core transaction without utxos
	ops := common.NewOptions(options)
	avax.SortTransferableOutputs(outputs, Parser.Codec()) // sort exported outputs

	utx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
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

	// feesMan cumulates complexity. Let's init it with utx filled so far
	feeCalc.TipPercentage = ops.TipPercentage()
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
		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only support [secp256k1fx.TransferOutput]s.
			continue
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

func (b *builder) financeTx(
	amountsToBurn map[ids.ID]uint64,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	changeOutputs []*avax.TransferableOutput,
	err error,
) {
	avaxAssetID := b.context.AVAXAssetID
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
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
		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			// We only support burning [secp256k1fx.TransferOutput]s.
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		input := &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			FxID:   secp256k1fx.ID,
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

func mintFTs(
	addresses set.Set[ids.ShortID],
	backend Backend,
	context *Context,
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := backend.UTXOs(options.Context(), context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(addresses)
	minIssuanceTime := options.MinIssuanceTime()

	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		output, ok := outputs[assetID]
		if !ok {
			continue
		}

		out, ok := utxo.Out.(*secp256k1fx.MintOutput)
		if !ok {
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &txs.Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
			FxID:    secp256k1fx.ID,
			Op: &secp256k1fx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
				MintOutput:     *out,
				TransferOutput: *output,
			},
		})

		if _, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(inputSigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}

		// remove the asset from the required outputs to mint
		delete(outputs, assetID)
	}

	for assetID := range outputs {
		return nil, fmt.Errorf(
			"%w: provided UTXOs not able to mint asset %q",
			errInsufficientFunds,
			assetID,
		)
	}
	return operations, nil
}

// TODO: make this able to generate multiple NFT groups
func mintNFTs(
	addresses set.Set[ids.ShortID],
	backend Backend,
	context *Context,
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := backend.UTXOs(options.Context(), context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(addresses)
	minIssuanceTime := options.MinIssuanceTime()

	for _, utxo := range utxos {
		if assetID != utxo.AssetID() {
			continue
		}

		out, ok := utxo.Out.(*nftfx.MintOutput)
		if !ok {
			// wrong output type
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &txs.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
			FxID: nftfx.ID,
			Op: &nftfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
				GroupID: out.GroupID,
				Payload: payload,
				Outputs: owners,
			},
		})

		if _, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(inputSigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}

		return operations, nil
	}
	return nil, fmt.Errorf(
		"%w: provided UTXOs not able to mint NFT %q",
		errInsufficientFunds,
		assetID,
	)
}

func mintProperty(
	addresses set.Set[ids.ShortID],
	backend Backend,
	context *Context,
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := backend.UTXOs(options.Context(), context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(addresses)
	minIssuanceTime := options.MinIssuanceTime()

	for _, utxo := range utxos {
		if assetID != utxo.AssetID() {
			continue
		}

		out, ok := utxo.Out.(*propertyfx.MintOutput)
		if !ok {
			// wrong output type
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &txs.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
			FxID: propertyfx.ID,
			Op: &propertyfx.MintOperation{
				MintInput: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
				MintOutput: *out,
				OwnedOutput: propertyfx.OwnedOutput{
					OutputOwners: *owner,
				},
			},
		})

		if _, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(inputSigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}

		return operations, nil
	}
	return nil, fmt.Errorf(
		"%w: provided UTXOs not able to mint property %q",
		errInsufficientFunds,
		assetID,
	)
}

func burnProperty(
	addresses set.Set[ids.ShortID],
	backend Backend,
	context *Context,
	assetID ids.ID,
	feeCalc *fees.Calculator,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := backend.UTXOs(options.Context(), context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(addresses)
	minIssuanceTime := options.MinIssuanceTime()

	for _, utxo := range utxos {
		if assetID != utxo.AssetID() {
			continue
		}

		out, ok := utxo.Out.(*propertyfx.OwnedOutput)
		if !ok {
			// wrong output type
			continue
		}

		inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &txs.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
			FxID: propertyfx.ID,
			Op: &propertyfx.BurnOperation{
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})

		if _, err = fees.FinanceCredential(feeCalc, Parser.Codec(), len(inputSigIndices)); err != nil {
			return nil, fmt.Errorf("account for credential fees: %w", err)
		}
	}
	if len(operations) == 0 {
		return nil, fmt.Errorf(
			"%w: provided UTXOs not able to burn property %q",
			errInsufficientFunds,
			assetID,
		)
	}
	return operations, nil
}

func (b *builder) initCtx(tx txs.UnsignedTx) error {
	ctx, err := NewSnowContext(
		b.context.NetworkID,
		b.context.BlockchainID,
		b.context.AVAXAssetID,
	)
	if err != nil {
		return err
	}

	tx.InitCtx(ctx)
	return nil
}
