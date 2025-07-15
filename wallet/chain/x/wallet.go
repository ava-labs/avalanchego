// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/chain/x/builder"
	"github.com/ava-labs/avalanchego/wallet/chain/x/signer"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var _ Wallet = (*wallet)(nil)

type Wallet interface {
	// Builder returns the builder that will be used to create the transactions.
	Builder() builder.Builder

	// Signer returns the signer that will be used to sign the transactions.
	Signer() signer.Signer

	// IssueBaseTx creates, signs, and issues a new simple value transfer.
	//
	// - [outputs] specifies all the recipients and amounts that should be sent
	//   from this transaction.
	IssueBaseTx(
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueCreateAssetTx creates, signs, and issues a new asset.
	//
	// - [name] specifies a human readable name for this asset.
	// - [symbol] specifies a human readable abbreviation for this asset.
	// - [denomination] specifies how many times the asset can be split. For
	//   example, a denomination of [4] would mean that the smallest unit of the
	//   asset would be 0.001 units.
	// - [initialState] specifies the supported feature extensions for this
	//   asset as well as the initial outputs for the asset.
	IssueCreateAssetTx(
		name string,
		symbol string,
		denomination byte,
		initialState map[uint32][]verify.State,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueOperationTx creates, signs, and issues state changes on the UTXO
	// set. These state changes may be more complex than simple value transfers.
	//
	// - [operations] specifies the state changes to perform.
	IssueOperationTx(
		operations []*txs.Operation,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueOperationTxMintFT creates, signs, and issues a set of state changes
	// that mint new tokens for the requested assets.
	//
	// - [outputs] maps the assetID to the output that should be created for the
	//   asset.
	IssueOperationTxMintFT(
		outputs map[ids.ID]*secp256k1fx.TransferOutput,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueOperationTxMintNFT creates, signs, and issues a state change that
	// mints new NFTs for the requested asset.
	//
	// - [assetID] specifies the asset to mint the NFTs under.
	// - [payload] specifies the payload to provide each new NFT.
	// - [owners] specifies the new owners of each NFT.
	IssueOperationTxMintNFT(
		assetID ids.ID,
		payload []byte,
		owners []*secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueOperationTxMintProperty creates, signs, and issues a state change
	// that mints a new property for the requested asset.
	//
	// - [assetID] specifies the asset to mint the property under.
	// - [owner] specifies the new owner of the property.
	IssueOperationTxMintProperty(
		assetID ids.ID,
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueOperationTxBurnProperty creates, signs, and issues state changes
	// that burns all the properties of the requested asset.
	//
	// - [assetID] specifies the asset to burn the property of.
	IssueOperationTxBurnProperty(
		assetID ids.ID,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueImportTx creates, signs, and issues an import transaction that
	// attempts to consume all the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	IssueImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueUnsignedTx signs and issues the unsigned tx.
	IssueUnsignedTx(
		utx txs.UnsignedTx,
		options ...common.Option,
	) (*txs.Tx, error)

	// IssueTx issues the signed tx.
	IssueTx(
		tx *txs.Tx,
		options ...common.Option,
	) error
}

func NewWallet(
	builder builder.Builder,
	signer signer.Signer,
	client *avm.Client,
	backend Backend,
) Wallet {
	return &wallet{
		backend: backend,
		builder: builder,
		signer:  signer,
		client:  client,
	}
}

type wallet struct {
	backend Backend
	builder builder.Builder
	signer  signer.Signer
	client  *avm.Client
}

func (w *wallet) Builder() builder.Builder {
	return w.builder
}

func (w *wallet) Signer() signer.Signer {
	return w.signer
}

func (w *wallet) IssueBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewBaseTx(outputs, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueCreateAssetTx(
	name string,
	symbol string,
	denomination byte,
	initialState map[uint32][]verify.State,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewCreateAssetTx(name, symbol, denomination, initialState, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueOperationTx(
	operations []*txs.Operation,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewOperationTx(operations, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueOperationTxMintFT(
	outputs map[ids.ID]*secp256k1fx.TransferOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewOperationTxMintFT(outputs, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueOperationTxMintNFT(
	assetID ids.ID,
	payload []byte,
	owners []*secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewOperationTxMintNFT(assetID, payload, owners, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueOperationTxMintProperty(
	assetID ids.ID,
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewOperationTxMintProperty(assetID, owner, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueOperationTxBurnProperty(
	assetID ids.ID,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewOperationTxBurnProperty(assetID, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueImportTx(
	chainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewImportTx(chainID, to, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (*txs.Tx, error) {
	utx, err := w.builder.NewExportTx(chainID, outputs, options...)
	if err != nil {
		return nil, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueUnsignedTx(
	utx txs.UnsignedTx,
	options ...common.Option,
) (*txs.Tx, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := signer.SignUnsigned(ctx, w.signer, utx)
	if err != nil {
		return nil, err
	}

	return tx, w.IssueTx(tx, options...)
}

func (w *wallet) IssueTx(
	tx *txs.Tx,
	options ...common.Option,
) error {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	startTime := time.Now()
	txID, err := w.client.IssueTx(ctx, tx.Bytes())
	if err != nil {
		return err
	}

	issuanceDuration := time.Since(startTime)
	if f := ops.IssuanceHandler(); f != nil {
		f(common.IssuanceReceipt{
			ChainAlias: builder.Alias,
			TxID:       txID,
			Duration:   issuanceDuration,
		})
	}

	if ops.AssumeDecided() {
		return w.backend.AcceptTx(ctx, tx)
	}

	if err := avm.AwaitTxAccepted(w.client, ctx, txID, ops.PollFrequency()); err != nil {
		return err
	}

	if f := ops.ConfirmationHandler(); f != nil {
		totalDuration := time.Since(startTime)
		confirmationDuration := totalDuration - issuanceDuration

		f(common.ConfirmationReceipt{
			ChainAlias:           builder.Alias,
			TxID:                 txID,
			TotalDuration:        totalDuration,
			ConfirmationDuration: confirmationDuration,
		})
	}

	return w.backend.AcceptTx(ctx, tx)
}
