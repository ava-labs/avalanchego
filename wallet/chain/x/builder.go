// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"errors"
	"fmt"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/avm"
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

	_ Builder = &builder{}
)

// Builder provides a convenient interface for building unsigned X-chain
// transactions.
type Builder interface {
	// NewBaseTx creates a new simple value transfer.
	//
	// - [outputs] specifies all the recipients and amounts that should be sent
	//   from this transaction.
	NewBaseTx(
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*avm.BaseTx, error)

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
	) (*avm.CreateAssetTx, error)

	// NewOperationTx performs state changes on the UTXO set. These state
	// changes may be more complex than simple value transfers.
	//
	// - [operations] specifies the state changes to perform.
	NewOperationTx(
		operations []*avm.Operation,
		options ...common.Option,
	) (*avm.OperationTx, error)

	// NewOperationTxMintFT performs a set of state changes that mint new tokens
	// for the requested assets.
	//
	// - [outputs] maps the assetID to the output that should be created for the
	//   asset.
	NewOperationTxMintFT(
		outputs map[ids.ID]*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*avm.OperationTx, error)

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
	) (*avm.OperationTx, error)

	// NewOperationTxMintProperty performs a state change that mints a new
	// property for the requested asset.
	//
	// - [assetID] specifies the asset to mint the property under.
	// - [owner] specifies the new owner of the property.
	NewOperationTxMintProperty(
		assetID ids.ID,
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*avm.OperationTx, error)

	// NewOperationTxBurnProperty performs state changes that burns all the
	// properties of the requested asset.
	//
	// - [assetID] specifies the asset to burn the property of.
	NewOperationTxBurnProperty(
		assetID ids.ID,
		options ...common.Option,
	) (*avm.OperationTx, error)

	// NewImportTx creates an import transaction that attempts to consume all
	// the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	NewImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*avm.ImportTx, error)

	// NewExportTx creates an export transaction that attempts to send all the
	// provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	NewExportTx(
		chainID ids.ID,
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*avm.ExportTx, error)
}

// BuilderBackend specifies the required information needed to build unsigned
// X-chain transactions.
type BuilderBackend interface {
	Context

	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
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

func (b *builder) NewBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*avm.BaseTx, error) {
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

	ops := common.NewOptions(options)
	inputs, changeOutputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}
	outputs = append(outputs, changeOutputs...)
	avax.SortTransferableOutputs(outputs, Codec) // sort the outputs

	return &avm.BaseTx{BaseTx: avax.BaseTx{
		NetworkID:    b.backend.NetworkID(),
		BlockchainID: b.backend.BlockchainID(),
		Ins:          inputs,
		Outs:         outputs,
		Memo:         ops.Memo(),
	}}, nil
}

func (b *builder) NewCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...common.Option,
) (*avm.CreateAssetTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateAssetTxFee(),
	}
	ops := common.NewOptions(options)
	inputs, outputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

	states := make([]*avm.InitialState, 0, len(initialState))
	for fxIndex, outs := range initialState {
		state := &avm.InitialState{
			FxIndex: fxIndex,
			Outs:    outs,
		}
		state.Sort(Codec) // sort the outputs
		states = append(states, state)
	}

	tx := &avm.CreateAssetTx{
		BaseTx: avm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: b.backend.BlockchainID(),
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Name:         name,
		Symbol:       symbol,
		Denomination: denomination,
		States:       states,
	}
	tx.Sort() // sort the initial states
	return tx, nil
}

func (b *builder) NewOperationTx(
	operations []*avm.Operation,
	options ...common.Option,
) (*avm.OperationTx, error) {
	toBurn := map[ids.ID]uint64{
		b.backend.AVAXAssetID(): b.backend.CreateAssetTxFee(),
	}
	ops := common.NewOptions(options)
	inputs, outputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

	avm.SortOperations(operations, Codec)
	return &avm.OperationTx{
		BaseTx: avm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: b.backend.BlockchainID(),
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		Ops: operations,
	}, nil
}

func (b *builder) NewOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*avm.OperationTx, error) {
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
) (*avm.OperationTx, error) {
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
) (*avm.OperationTx, error) {
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
) (*avm.OperationTx, error) {
	ops := common.NewOptions(options)
	operations, err := b.burnProperty(assetID, ops)
	if err != nil {
		return nil, err
	}
	return b.NewOperationTx(operations, options...)
}

func (b *builder) NewImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (
	*avm.ImportTx,
	error,
) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), sourceChainID)
	if err != nil {
		return nil, err
	}

	var (
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.backend.AVAXAssetID()
		txFee           = b.backend.BaseTxFee()

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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
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
	}
	avax.SortTransferableInputs(importedInputs) // sort imported inputs

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
			Out: &secp256k1fx.TransferOutput{
				Amt:          amount,
				OutputOwners: *to,
			},
		})
	}

	avax.SortTransferableOutputs(outputs, Codec)
	return &avm.ImportTx{
		BaseTx: avm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: constants.PlatformChainID,
			Ins:          inputs,
			Outs:         outputs,
			Memo:         ops.Memo(),
		}},
		SourceChain: sourceChainID,
		ImportedIns: importedInputs,
	}, nil
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*avm.ExportTx, error) {
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

	ops := common.NewOptions(options)
	inputs, changeOutputs, err := b.spend(toBurn, ops)
	if err != nil {
		return nil, err
	}

	avax.SortTransferableOutputs(outputs, Codec)
	return &avm.ExportTx{
		BaseTx: avm.BaseTx{BaseTx: avax.BaseTx{
			NetworkID:    b.backend.NetworkID(),
			BlockchainID: b.backend.BlockchainID(),
			Ins:          inputs,
			Outs:         changeOutputs,
			Memo:         ops.Memo(),
		}},
		DestinationChain: chainID,
		ExportedOuts:     outputs,
	}, nil
}

func (b *builder) spend(
	amountsToBurn map[ids.ID]uint64,
	options *common.Options,
) (
	inputs []*avax.TransferableInput,
	outputs []*avax.TransferableOutput,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.backend.BlockchainID())
	if err != nil {
		return nil, nil, err
	}

	minIssuanceTime := options.MinIssuanceTime()

	addr, ok := b.addrs.Peek()
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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
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
		if remainingAmount := out.Amt - amountToBurn; remainingAmount > 0 {
			// This input had extra value, so some of it must be returned
			outputs = append(outputs, &avax.TransferableOutput{
				Asset: utxo.Asset,
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

	avax.SortTransferableInputs(inputs)          // sort inputs
	avax.SortTransferableOutputs(outputs, Codec) // sort the change outputs
	return inputs, outputs, nil
}

func (b *builder) mintFTs(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options *common.Options,
) (
	operations []*avm.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.backend.BlockchainID())
	if err != nil {
		return nil, err
	}

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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &avm.Operation{
			Asset:   utxo.Asset,
			UTXOIDs: []*avax.UTXOID{&utxo.UTXOID},
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
	operations []*avm.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.backend.BlockchainID())
	if err != nil {
		return nil, err
	}

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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &avm.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
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
	operations []*avm.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.backend.BlockchainID())
	if err != nil {
		return nil, err
	}

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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &avm.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
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
	operations []*avm.Operation,
	err error,
) {
	utxos, err := b.backend.UTXOs(options.Context(), b.backend.BlockchainID())
	if err != nil {
		return nil, err
	}

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

		inputSigIndices, ok := b.match(&out.OutputOwners, minIssuanceTime)
		if !ok {
			continue
		}

		// add the operation to the array
		operations = append(operations, &avm.Operation{
			Asset: avax.Asset{ID: assetID},
			UTXOIDs: []*avax.UTXOID{
				&utxo.UTXOID,
			},
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

// match attempts to match a list of addresses up to the provided threshold
func (b *builder) match(owners *secp256k1fx.OutputOwners, minIssuanceTime uint64) ([]uint32, bool) {
	if owners.Locktime > minIssuanceTime {
		return nil, false
	}

	sigs := make([]uint32, 0, owners.Threshold)
	for i := uint32(0); i < uint32(len(owners.Addrs)) && uint32(len(sigs)) < owners.Threshold; i++ {
		addr := owners.Addrs[i]
		if b.addrs.Contains(addr) {
			sigs = append(sigs, i)
		}
	}
	return sigs, uint32(len(sigs)) == owners.Threshold
}
