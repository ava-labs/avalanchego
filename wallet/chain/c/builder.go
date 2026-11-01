// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"math/big"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/math/intmath"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	ethcommon "github.com/ava-labs/libevm/common"
)

var (
	_ Builder = (*builder)(nil)

	errInsufficientFunds = errors.New("insufficient funds")
)

// Builder provides a convenient interface for building unsigned C-chain
// transactions.
type Builder interface {
	// Context returns the configuration of the chain that this builder uses to
	// create transactions.
	Context() *Context

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
	// - [baseFee] specifies the fee price, in aAVAX/gas, willing to be paid by
	//   this tx.
	NewImportTx(
		chainID ids.ID,
		to ethcommon.Address,
		baseFee gas.Price,
		options ...common.Option,
	) (*tx.Import, error)

	// NewExportTx creates an export transaction that attempts to send all the
	// provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	// - [baseFee] specifies the fee price, in aAVAX/gas, willing to be paid by
	//   this tx.
	NewExportTx(
		chainID ids.ID,
		outputs []*secp256k1fx.TransferOutput,
		baseFee gas.Price,
		options ...common.Option,
	) (*tx.Export, error)
}

// BuilderBackend specifies the required information needed to build unsigned
// C-chain transactions.
type BuilderBackend interface {
	UTXOs(ctx context.Context, sourceChainID ids.ID) ([]*avax.UTXO, error)
	Balance(ctx context.Context, addr ethcommon.Address) (*big.Int, error)
	Nonce(ctx context.Context, addr ethcommon.Address) (uint64, error)
}

type builder struct {
	avaxAddrs set.Set[ids.ShortID]
	ethAddrs  set.Set[ethcommon.Address]
	context   *Context
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
	context *Context,
	backend BuilderBackend,
) Builder {
	return &builder{
		avaxAddrs: avaxAddrs,
		ethAddrs:  ethAddrs,
		context:   context,
		backend:   backend,
	}
}

func (b *builder) Context() *Context {
	return b.context
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
		avaxAssetID     = b.context.AVAXAssetID
		balance         uint64
	)
	for _, utxo := range utxos {
		amount, _, ok := getSpendableAmount(utxo, addrs, minIssuanceTime, avaxAssetID)
		if !ok {
			continue
		}

		newBalance, err := math.Add(balance, amount)
		if err != nil {
			return 0, err
		}
		balance = newBalance
	}

	return balance, nil
}

func (b *builder) NewImportTx(
	chainID ids.ID,
	to ethcommon.Address,
	baseFee gas.Price,
	options ...common.Option,
) (*tx.Import, error) {
	ops := common.NewOptions(options)
	utxos, err := b.backend.UTXOs(ops.Context(), chainID)
	if err != nil {
		return nil, err
	}

	var (
		addrs           = ops.Addresses(b.avaxAddrs)
		minIssuanceTime = ops.MinIssuanceTime()
		avaxAssetID     = b.context.AVAXAssetID

		importedInputs = make([]*avax.TransferableInput, 0, len(utxos))
		importedAmount uint64
	)
	for _, utxo := range utxos {
		amount, inputSigIndices, ok := getSpendableAmount(utxo, addrs, minIssuanceTime, avaxAssetID)
		if !ok {
			continue
		}

		importedInputs = append(importedInputs, &avax.TransferableInput{
			UTXOID: utxo.UTXOID,
			Asset:  utxo.Asset,
			FxID:   secp256k1fx.ID,
			In: &secp256k1fx.TransferInput{
				Amt: amount,
				Input: secp256k1fx.Input{
					SigIndices: inputSigIndices,
				},
			},
		})

		newImportedAmount, err := math.Add(importedAmount, amount)
		if err != nil {
			return nil, err
		}
		importedAmount = newImportedAmount
	}

	utils.Sort(importedInputs)
	utx := &tx.Import{
		NetworkID:      b.context.NetworkID,
		BlockchainID:   b.context.BlockchainID,
		SourceChain:    chainID,
		ImportedInputs: importedInputs,
		Outs: []tx.Output{{
			Address: to,
			AssetID: avaxAssetID,
			// Amount set below.
		}},
	}

	gasUsed, err := tx.GasUsed(utx)
	if err != nil {
		return nil, err
	}
	txFee, err := calculateFee(gasUsed, baseFee)
	if err != nil {
		return nil, err
	}

	if importedAmount <= txFee {
		return nil, errInsufficientFunds
	}
	utx.Outs[0].Amount = importedAmount - txFee
	return utx, nil
}

func (b *builder) NewExportTx(
	chainID ids.ID,
	outputs []*secp256k1fx.TransferOutput,
	baseFee gas.Price,
	options ...common.Option,
) (*tx.Export, error) {
	var (
		avaxAssetID     = b.context.AVAXAssetID
		exportedOutputs = make([]*avax.TransferableOutput, len(outputs))
		exportedAmount  uint64
	)
	for i, output := range outputs {
		exportedOutputs[i] = &avax.TransferableOutput{
			Asset: avax.Asset{ID: avaxAssetID},
			FxID:  secp256k1fx.ID,
			Out:   output,
		}

		newExportedAmount, err := math.Add(exportedAmount, output.Amt)
		if err != nil {
			return nil, err
		}
		exportedAmount = newExportedAmount
	}

	tx.SortExportedOutputs(exportedOutputs)
	utx := &tx.Export{
		NetworkID:        b.context.NetworkID,
		BlockchainID:     b.context.BlockchainID,
		DestinationChain: chainID,
		ExportedOutputs:  exportedOutputs,
	}

	gasUsed, err := tx.GasUsed(utx)
	if err != nil {
		return nil, err
	}
	fee, err := calculateFee(gasUsed, baseFee)
	if err != nil {
		return nil, err
	}

	amountToConsume, err := math.Add(exportedAmount, fee)
	if err != nil {
		return nil, err
	}

	// Inputs are fixed-size, so every input adds the same amount of gas.
	utx.Ins = make([]tx.Input, 1)
	gasUsedWithInput, err := tx.GasUsed(utx)
	if err != nil {
		return nil, err
	}
	inputGas := gasUsedWithInput - gasUsed

	var (
		ops   = common.NewOptions(options)
		ctx   = ops.Context()
		addrs = ops.EthAddresses(b.ethAddrs)
	)
	utx.Ins = make([]tx.Input, 0, addrs.Len())
	for addr := range addrs {
		if amountToConsume == 0 {
			break
		}

		newGasUsed, err := math.Add(gasUsed, inputGas)
		if err != nil {
			return nil, err
		}
		newFee, err := calculateFee(newGasUsed, baseFee)
		if err != nil {
			return nil, err
		}

		additionalFee := newFee - fee

		balance, err := b.backend.Balance(ctx, addr)
		if err != nil {
			return nil, err
		}

		// Since the asset is AVAX, we divide by [tx.X2CRate] to convert back to
		// the correct denomination of AVAX that can be exported.
		avaxBalance := new(big.Int).Div(balance, big.NewInt(tx.X2CRate)).Uint64()

		// If the balance for [addr] is insufficient to cover the additional
		// cost of adding an input to the transaction, skip adding the input
		// altogether.
		if avaxBalance <= additionalFee {
			continue
		}

		// Update the gas used and fee for the next iteration
		gasUsed = newGasUsed
		fee = newFee

		amountToConsume, err = math.Add(amountToConsume, additionalFee)
		if err != nil {
			return nil, err
		}

		nonce, err := b.backend.Nonce(ctx, addr)
		if err != nil {
			return nil, err
		}

		inputAmount := min(amountToConsume, avaxBalance)
		utx.Ins = append(utx.Ins, tx.Input{
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

	utils.Sort(utx.Ins)

	snowCtx, err := newSnowContext(b.context)
	if err != nil {
		return nil, err
	}
	for _, out := range utx.ExportedOutputs {
		out.InitCtx(snowCtx)
	}
	return utx, nil
}

func getSpendableAmount(
	utxo *avax.UTXO,
	addrs set.Set[ids.ShortID],
	minIssuanceTime uint64,
	avaxAssetID ids.ID,
) (uint64, []uint32, bool) {
	if utxo.Asset.ID != avaxAssetID {
		// Only AVAX can be imported
		return 0, nil, false
	}

	out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
	if !ok {
		// Can't import an unknown transfer output type
		return 0, nil, false
	}

	inputSigIndices, ok := common.MatchOwners(&out.OutputOwners, addrs, minIssuanceTime)
	return out.Amt, inputSigIndices, ok
}

// calculateFee returns the minimum amount of nAVAX that a transaction consuming
// gasUsed MUST burn for its gas price to be at least price, in aAVAX/gas.
//
// calculateFee returns an error if gasUsed*price overflows a uint64, so it
// can't compute fees above ~18.4 AVAX.
func calculateFee(gasUsed gas.Gas, price gas.Price) (uint64, error) {
	cost, err := gasUsed.Cost(price)
	if err != nil {
		return 0, err
	}
	// The C-Chain computes the gas price by rounding down, so the fee must be
	// rounded up.
	return intmath.CeilDiv(cost, tx.X2CRate), nil
}
