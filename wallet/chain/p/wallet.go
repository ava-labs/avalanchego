// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

var (
	errNotCommitted = errors.New("not committed")

	_ Wallet = &wallet{}
)

type Wallet interface {
	Context

	// Builder returns the builder that will be used to create the transactions.
	Builder() Builder

	// Signer returns the signer that will be used to sign the transactions.
	Signer() Signer

	// IssueBaseTx creates, signs, and issues a new simple value transfer.
	// Because the P-chain doesn't intend for balance transfers to occur, this
	// method is expensive and abuses the creation of subnets.
	//
	// - [outputs] specifies all the recipients and amounts that should be sent
	//   from this transaction.
	IssueBaseTx(
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (ids.ID, error)

	// IssueAddValidatorTx creates, signs, and issues a new validator of the
	// primary network.
	//
	// - [validator] specifies all the details of the validation period such as
	//   the startTime, endTime, stake weight, and nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this validator
	//   may accrue during its validation period.
	// - [shares] specifies the fraction (out of 1,000,000) that this validator
	//   will take from delegation rewards. If 1,000,000 is provided, 100% of
	//   the delegation reward will be sent to the validator's [rewardsOwner].
	IssueAddValidatorTx(
		validator *platformvm.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		shares uint32,
		options ...common.Option,
	) (ids.ID, error)

	// IssueAddSubnetValidatorTx creates, signs, and issues a new validator of a
	// subnet.
	//
	// - [validator] specifies all the details of the validation period such as
	//   the startTime, endTime, sampling weight, nodeID, and subnetID.
	IssueAddSubnetValidatorTx(
		validator *platformvm.SubnetValidator,
		options ...common.Option,
	) (ids.ID, error)

	// IssueAddDelegatorTx creates, signs, and issues a new delegator to a
	// validator on the primary network.
	//
	// - [validator] specifies all the details of the delegation period such as
	//   the startTime, endTime, stake weight, and validator's nodeID.
	// - [rewardsOwner] specifies the owner of all the rewards this delegator
	//   may accrue at the end of its delegation period.
	IssueAddDelegatorTx(
		validator *platformvm.Validator,
		rewardsOwner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (ids.ID, error)

	// IssueCreateChainTx creates, signs, and issues a new chain in the named
	// subnet.
	//
	// - [subnetID] specifies the subnet to launch the chain in.
	// - [genesis] specifies the initial state of the new chain.
	// - [vmID] specifies the vm that the new chain will run.
	// - [fxIDs] specifies all the feature extensions that the vm should be
	//   running with.
	// - [chainName] specifies a human readable name for the chain.
	IssueCreateChainTx(
		subnetID ids.ID,
		genesis []byte,
		vmID ids.ID,
		fxIDs []ids.ID,
		chainName string,
		options ...common.Option,
	) (ids.ID, error)

	// IssueCreateSubnetTx creates, signs, and issues a new subnet with the
	// specified owner.
	//
	// - [owner] specifies who has the ability to create new chains and add new
	//   validators to the subnet.
	IssueCreateSubnetTx(
		owner *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (ids.ID, error)

	// IssueImportTx creates, signs, and issues an import transaction that
	// attempts to consume all the available UTXOs and import the funds to [to].
	//
	// - [chainID] specifies the chain to be importing funds from.
	// - [to] specifies where to send the imported funds to.
	IssueImportTx(
		chainID ids.ID,
		to *secp256k1fx.OutputOwners,
		options ...common.Option,
	) (ids.ID, error)

	// IssueExportTx creates, signs, and issues an export transaction that
	// attempts to send all the provided [outputs] to the requested [chainID].
	//
	// - [chainID] specifies the chain to be exporting the funds to.
	// - [outputs] specifies the outputs to send to the [chainID].
	IssueExportTx(
		chainID ids.ID,
		outputs []*avax.TransferableOutput,
		options ...common.Option,
	) (ids.ID, error)

	// IssueUnsignedTx signs and issues the unsigned tx.
	IssueUnsignedTx(
		utx platformvm.UnsignedTx,
		options ...common.Option,
	) (ids.ID, error)

	// IssueTx issues the signed tx.
	IssueTx(
		tx *platformvm.Tx,
		options ...common.Option,
	) (ids.ID, error)
}

func NewWallet(
	builder Builder,
	signer Signer,
	client platformvm.Client,
	backend Backend,
) Wallet {
	return &wallet{
		Backend: backend,
		builder: builder,
		signer:  signer,
		client:  client,
	}
}

type wallet struct {
	Backend
	builder Builder
	signer  Signer
	client  platformvm.Client
}

func (w *wallet) Builder() Builder { return w.builder }

func (w *wallet) Signer() Signer { return w.signer }

func (w *wallet) IssueBaseTx(
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewBaseTx(outputs, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddValidatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	shares uint32,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewAddValidatorTx(validator, rewardsOwner, shares, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddSubnetValidatorTx(
	validator *platformvm.SubnetValidator,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewAddSubnetValidatorTx(validator, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueAddDelegatorTx(
	validator *platformvm.Validator,
	rewardsOwner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewAddDelegatorTx(validator, rewardsOwner, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueCreateChainTx(
	subnetID ids.ID,
	genesis []byte,
	vmID ids.ID,
	fxIDs []ids.ID,
	chainName string,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewCreateChainTx(subnetID, genesis, vmID, fxIDs, chainName, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueCreateSubnetTx(
	owner *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewCreateSubnetTx(owner, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueImportTx(
	sourceChainID ids.ID,
	to *secp256k1fx.OutputOwners,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewImportTx(sourceChainID, to, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueExportTx(
	chainID ids.ID,
	outputs []*avax.TransferableOutput,
	options ...common.Option,
) (ids.ID, error) {
	utx, err := w.builder.NewExportTx(chainID, outputs, options...)
	if err != nil {
		return ids.Empty, err
	}
	return w.IssueUnsignedTx(utx, options...)
}

func (w *wallet) IssueUnsignedTx(
	utx platformvm.UnsignedTx,
	options ...common.Option,
) (ids.ID, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	tx, err := w.signer.SignUnsigned(ctx, utx)
	if err != nil {
		return ids.Empty, err
	}

	return w.IssueTx(tx, options...)
}

func (w *wallet) IssueTx(
	tx *platformvm.Tx,
	options ...common.Option,
) (ids.ID, error) {
	ops := common.NewOptions(options)
	ctx := ops.Context()
	txID, err := w.client.IssueTx(ctx, tx.Bytes())
	if err != nil {
		return ids.Empty, err
	}

	if ops.AssumeDecided() {
		return txID, w.Backend.AcceptTx(ctx, tx)
	}

	txStatus, err := w.client.AwaitTxDecided(ctx, txID, false, ops.PollFrequency())
	if err != nil {
		return txID, err
	}

	if err := w.Backend.AcceptTx(ctx, tx); err != nil {
		return txID, err
	}

	if txStatus.Status != status.Committed {
		return txID, errNotCommitted
	}
	return txID, nil
}
