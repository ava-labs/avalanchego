// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"errors"
	"math/big"

	stdcontext "context"

	"github.com/ava-labs/coreth/plugin/evm"

	ethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

const avaxConversionRateInt = 1_000_000_000

var (
	_ Builder = (*builder)(nil)

	errInsufficientFunds = errors.New("insufficient funds")

	// avaxConversionRate is the conversion rate between the smallest
	// denomination on the X-Chain and P-chain, 1 nAVAX, and the smallest
	// denomination on the C-Chain 1 wei. Where 1 nAVAX = 1 gWei.
	//
	// This is only required for AVAX because the denomination of 1 AVAX is 9
	// decimal places on the X and P chains, but is 18 decimal places within the
	// EVM.
	avaxConversionRate = big.NewInt(avaxConversionRateInt)
)

// Builder provides a convenient interface for building unsigned C-chain
// transactions.
type Builder interface {
	// GetBalance calculates the amount of AVAX that this builder has control
	// over.
	GetBalance(
		options ...common.Option,
	) (*big.Int, error)

	// GetImportableBalance calculates the amount of AVAX that this builder
	// could import from the provided chain.
	//
	// - [chainID] specifies the chain the funds are from.
	GetImportableBalance(
		chainID ids.ID,
		options ...common.Option,
	) (uint64, error)

	// NewImportTx creates an import transaction that attempts to consume all
	// the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	// - [baseFee] specifies the fee price willing to be paid by this tx.
	NewImportTx(
		chainID ids.ID,
		to ethcommon.Address,
		baseFee *big.Int,
		options ...common.Option,
	) (*evm.UnsignedImportTx, error)

	// NewExportTx creates an export transaction that attempts to send all the
	// provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	// - [baseFee] specifies the fee price willing to be paid by this tx.
	NewExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		baseFee *big.Int,
		options ...common.Option,
	) (*evm.UnsignedExportTx, error)
}

// BuilderBackend specifies the required information needed to build unsigned
// C-chain transactions.
type BuilderBackend interface {
	Context

	UTXOs(ctx stdcontext.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	Balance(ctx stdcontext.Context, addr ethcommon.Address) (*big.Int, error)
	Nonce(ctx stdcontext.Context, addr ethcommon.Address) (uint64, error)
}

type builder struct {
	avaxAddrs set.Set[ids.ShortID]
	ethAddrs  set.Set[ethcommon.Address]
	backend   BuilderBackend
}

// NewBuilder returns a new transaction builder.
//
//   - [avaxAddrs] is the set of addresses in the AVAX format that the builder
//     assumes can be used when signing the transactions in the future.
//   - [ethAddrs] is the set of addresses in the Eth format that the builder
//     assumes can be used when signing the transactions in the future.
//   - [backend] provides the required access to the chain's context and state
//     to build out the transactions.
func NewBuilder(
	avaxAddrs set.Set[ids.ShortID],
	ethAddrs set.Set[ethcommon.Address],
	backend BuilderBackend,
) Builder {
	return &builder{
		avaxAddrs: avaxAddrs,
		ethAddrs:  ethAddrs,
		backend:   backend,
	}
}

func (b *builder) GetBalance(
	options ...common.Option,
) (*big.Int, error) {
	var (
		ops          = common.NewOptions(options)
		ctx          = ops.Context()
		addrs        = ops.EthAddresses(b.ethAddrs)
		totalBalance = new(big.Int)
	)
	for addr := range addrs {
		balance, err := b.backend.Balance(ctx, addr)
		if err != nil {
			return nil, err
		}
		totalBalance.Add(totalBalance, balance)
	}

	return totalBalance, nil
}

func (b *builder) GetImportableBalance(
	chainID ids.ID,
	options ...common.Option,
) (uint64, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), chainID)
	if err != nil {
		return 0, err
	}

	var (
		addrs           = ops.Addresses(b.avaxAddrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.backend.AVAXAssetID()
		amount          uint64
	)
	for _, utxo := range utxos {
		if utxo.Asset.ID != avaxAssetID {
			// Only AVAX can be imported
			continue
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			// Can't import an unknown transfer output type
			continue
		}

		_, ok = common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
		if !ok {
			// We couldn't spend this UTXO, so we skip to the next one
			continue
		}

		newAmount, err := math.Add64(amount, out.Amt)
		if err != nil {
			return 0, err
		}
		amount = newAmount
	}

	return amount, nil
}

func (b *builder) NewImportTx(
	chainID ids.ID,
	to ethcommon.Address,
	baseFee *big.Int,
	options ...common.Option,
) (*evm.UnsignedImportTx, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), chainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.avaxAddrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.backend.AVAXAssetID()

		importedInputs = make([]*avax.TransferableInput, 0, len(utxos))
		importedAmount uint64
	)
	for _, utxo := range utxos {
		if utxo.Asset.ID != avaxAssetID {
			// Only AVAX can be imported
			continue
		}

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

	utils.Sort(importedInputs)
	tx := &evm.UnsignedImportTx{
		NetworkID:      b.backend.NetworkID(),
		BlockchainID:   b.backend.BlockchainID(),
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
	}

	// We must initialize the bytes of the tx to calculate the initial cost
	wrappedTx := &evm.Tx{UnsignedAtomicTx: tx}
	if err := wrappedTx.Sign(evm.Codec, nil); err != nil {
		return nil, err
	}

	gasUsedWithoutOutput, err := tx.GasUsed(true /*=IsApricotPhase5*/)
	if err != nil {
		return nil, err
	}
	gasUsedWithOutput := gasUsedWithoutOutput + evm.EVMOutputGas

	txFee, err := evm.CalculateDynamicFee(gasUsedWithOutput, baseFee)
	if err != nil {
		return nil, err
	}

	if importedAmount <= txFee {
		return nil, errInsufficientFunds
	}

	tx.Outs = []evm.EVMOutput{{
		Address: to,
		Amount:  importedAmount - txFee,
		AssetID: avaxAssetID,
	}}
	return tx, nil
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	baseFee *big.Int,
	options ...common.Option,
) (*evm.UnsignedExportTx, error) {
	var (
		avaxAssetID     = b.backend.AVAXAssetID()
		exportedOutputs = make([]*avax.TransferableOutput, len(outputs))
		exportedAmount  uint64
	)
	for i, output := range outputs {
		exportedOutputs[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			Out:   output,
		}

		newExportedAmount, err := math.Add64(exportedAmount, output.Amt)
		if err != nil {
			return nil, err
		}
		exportedAmount = newExportedAmount
	}

	avax.SortTransferableOutputs(exportedOutputs, evm.Codec)
	tx := &evm.UnsignedExportTx{
		NetworkID:        b.backend.NetworkID(),
		BlockchainID:     b.backend.BlockchainID(),
		DestinationChain: chainID,
		ExportedOutputs:  exportedOutputs,
	}

	// We must initialize the bytes of the tx to calculate the initial cost
	wrappedTx := &evm.Tx{UnsignedAtomicTx: tx}
	if err := wrappedTx.Sign(evm.Codec, nil); err != nil {
		return nil, err
	}

	cost, err := tx.GasUsed(true /*=IsApricotPhase5*/)
	if err != nil {
		return nil, err
	}

	initialFee, err := evm.CalculateDynamicFee(cost, baseFee)
	if err != nil {
		return nil, err
	}

	amountToConsume, err := math.Add64(exportedAmount, initialFee)
	if err != nil {
		return nil, err
	}

	var (
		ops    = common.NewOptions(options)
		ctx    = ops.Context()
		addrs  = ops.EthAddresses(b.ethAddrs)
		inputs = make([]evm.EVMInput, 0, addrs.Len())
	)
	for addr := range addrs {
		if amountToConsume == 0 {
			break
		}

		prevFee, err := evm.CalculateDynamicFee(cost, baseFee)
		if err != nil {
			return nil, err
		}

		newCost := cost + evm.EVMInputGas
		newFee, err := evm.CalculateDynamicFee(newCost, baseFee)
		if err != nil {
			return nil, err
		}

		additionalFee := newFee - prevFee

		balance, err := b.backend.Balance(ctx, addr)
		if err != nil {
			return nil, err
		}

		// Since the asset is AVAX, we divide by the avaxConversionRate to
		// convert back to the correct denomination of AVAX that can be
		// exported.
		avaxBalance := new(big.Int).Div(balance, avaxConversionRate).Uint64()

		// If the balance for [addr] is insufficient to cover the additional
		// cost of adding an input to the transaction, skip adding the input
		// altogether.
		if avaxBalance <= additionalFee {
			continue
		}

		// Update the cost for the next iteration
		cost = newCost

		amountToConsume, err = math.Add64(amountToConsume, additionalFee)
		if err != nil {
			return nil, err
		}

		nonce, err := b.backend.Nonce(ctx, addr)
		if err != nil {
			return nil, err
		}

		inputAmount := math.Min(amountToConsume, avaxBalance)
		inputs = append(inputs, evm.EVMInput{
			Address: addr,
			Amount:  inputAmount,
			AssetID: avaxAssetID,
			Nonce:   nonce,
		})
		amountToConsume -= inputAmount
	}

	if amountToConsume > 0 {
		return nil, errInsufficientFunds
	}

	utils.Sort(inputs)
	tx.Ins = inputs
	return tx, nil
}
