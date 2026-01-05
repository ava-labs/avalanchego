// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package builder

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
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
		options ...common.Option,
	) (*txs.CreateAssetTx, error)

	// NewOperationTx performs state changes on the UTXO set. These state
	// changes may be more complex than simple value transfers.
	//
	// - [operations] specifies the state changes to perform.
	NewOperationTx(
		operations []*txs.Operation,
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxMintFT performs a set of state changes that mint new tokens
	// for the requested assets.
	//
	// - [outputs] maps the assetID to the output that should be created for the
	//   asset.
	NewOperationTxMintFT(
		outputs map[ids.ID]*secp256k1fx.TransferOutput,
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
		options ...common.Option,
	) (*txs.OperationTx, error)

	// NewOperationTxBurnProperty performs state changes that burns all the
	// properties of the requested asset.
	//
	// - [assetID] specifies the asset to burn the property of.
	NewOperationTxBurnProperty(
		assetID ids.ID,
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
	options ...common.Option,
) (*txs.BaseTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.BaseTxFee,
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	ops := common.NewOptions(options)
	inputs, changeOutputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}
	outputs = append(outputs, changeOutputs...)
	avax.SortTransferableOutputs(outputs, Parser.Codec()) // sort the outputs

	tx := &txs.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    b.context.NetworkID,
		BlockchainID: b.context.BlockchainID,
		Ins:          inputs,
		Outs:         outputs,
		Memo:         ops.Memo(),
	}}
	return tx, b.initCtx(tx)
}

func (b *builder) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...common.Option,
) (*txs.CreateAssetTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.CreateAssetTxFee,
	}
	ops := common.NewOptions(options)
	inputs, outputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

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
	tx := &txs.CreateAssetTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		States:       states,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewOperationTx(
	operations []*txs.Operation,
	options ...common.Option,
) (*txs.OperationTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.BaseTxFee,
	}
	ops := common.NewOptions(options)
	inputs, outputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

	txs.SortOperations(operations, Parser.Codec())
	tx := &txs.OperationTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Ops: operations,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := b.mintFTs(outputs, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, options...)
}

func (b *builder) NewOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := b.mintNFTs(assetID, payload, owners, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, options...)
}

func (b *builder) NewOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := b.mintProperty(assetID, owner, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, options...)
}

func (b *builder) NewOperationTxBurnProperty(
	assetID ids.ID,
	options ...common.Option,
) (*txs.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := b.burnProperty(assetID, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, options...)
}

func (b *builder) NewImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.ImportTx, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), chainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.addrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.context.AVAXAssetID
		txFee           = b.context.BaseTxFee

		importedInputs  = make([]*avax.TransferableInput, 0, len(utxos))
		importedAmounts = make(map[ids.ID]uint64)
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
		newImportedAmount, err := math.Add(importedAmounts[assetID], out.Amt)
		if err != nil {
			return nil, err
		}
		importedAmounts[assetID] = newImportedAmount
	}
	utils.Sort(importedInputs) // sort imported inputs

	if len(importedAmounts) == 0 {
		return nil, fmt.Errorf(
			"%w: no UTXOs available to import",
			errInsufficientFunds,
		)
	}

	var (
		inputs       []*avax.TransferableInput
		outputs      = make([]*avax.TransferableOutput, 0, len(importedAmounts))
		importedAVAX = importedAmounts[avaxAssetID]
	)
	if importedAVAX > txFee {
		importedAmounts[avaxAssetID] -= txFee
	} else {
		if importedAVAX < txFee { // imported amount goes toward paying tx fee
			toBurn := map[ids.ID]uint64{
				avaxAssetID: txFee - importedAVAX,
			}
			var err error
			inputs, outputs, err = b.spend(toBurn, ops)
			if err != nil {
				return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
			}
		}
		delete(importedAmounts, avaxAssetID)
	}

	for assetID, amount := range importedAmounts {
		outputs = append(outputs, &avax.TransferableOutput{
			Asset: avax.Asset{ID: assetID},
			FxID:  secp256k1fx.ID,
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *to,
			},
		})
	}

	avax.SortTransferableOutputs(outputs, Parser.Codec())
	tx := &txs.ImportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		SourceChain: chainID,
		ImportedIns: importedInputs,
	}
	return tx, b.initCtx(tx)
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.ExportTx, error) {
	toBurn := map[ids.ID]uint64{
		b.context.AVAXAssetID: b.context.BaseTxFee,
	}
	for _, out := range outputs {
		assetID := out.AssetID()
		amountToBurn, err := math.Add(toBurn[assetID], out.Out.Amount())
		if err != nil {
			return nil, err
		}
		toBurn[assetID] = amountToBurn
	}

	ops := common.NewOptions(options)
	inputs, changeOutputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

	avax.SortTransferableOutputs(outputs, Parser.Codec())
	tx := &txs.ExportTx{
		BaseTx: txs.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.context.NetworkID,
			BlockchainID: b.context.BlockchainID,
			Ins:          inputs,
			Outs:         changeOutputs,
			Memo:         ops.Memo(),
		}},
		DestinationChain: chainID,
		ExportedOuts:     outputs,
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
		balance[assetID], err = math.Add(balance[assetID], out.Amt)
		if err != nil {
			return nil, err
		}
	}
	return balance, nil
}

func (b *builder) spend(
	amountsToBurn map[ids.ID]uint64,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	outputs []*avax.TransferableOutput,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
	if err != nil {
		return nil, nil, err
	}

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

	// Iterate over the UTXOs
	for _, utxo := range utxos {
		assetID := utxo.AssetID()
		remainingAmountToBurn := amountsToBurn[assetID]

		// If we have consumed enough of the asset, then we have no need burn
		// more.
		if remainingAmountToBurn == 0 {
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

		inputs = append(inputs, &avax.TransferableInput{
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

		// Burn any value that should be burned
		amountToBurn := min(
			remainingAmountToBurn, // Amount we still need to burn
			out.Amt,               // Amount available to burn
		)
		amountsToBurn[assetID] -= amountToBurn
		if remainingAmount := out.Amt - amountToBurn; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned
			outputs = append(outputs, &avax.TransferableOutput{
				Asset: utxo.Asset,
				FxID:  secp256k1fx.ID,
				Out: &secp256k1fx.TransferOutput{
					Amt:          remainingAmount,
					OutputOwners: *changeOwner,
				},
			})
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

	utils.Sort(inputs)                                    // sort inputs
	avax.SortTransferableOutputs(outputs, Parser.Codec()) // sort the change outputs
	return inputs, outputs, nil
}

func (b *builder) mintFTs(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(b.addrs)
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
func (b *builder) mintNFTs(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(b.addrs)
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
		return operations, nil
	}
	return nil, fmt.Errorf(
		"%w: provided UTXOs not able to mint NFT %q",
		errInsufficientFunds,
		assetID,
	)
}

func (b *builder) mintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(b.addrs)
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
		return operations, nil
	}
	return nil, fmt.Errorf(
		"%w: provided UTXOs not able to mint property %q",
		errInsufficientFunds,
		assetID,
	)
}

func (b *builder) burnProperty(
	assetID ids.ID,
	options *common.Options,
) (
	operations []*txs.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.context.BlockchainID)
	if err != nil {
		return nil, err
	}

	addrs := options.Addresses(b.addrs)
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
