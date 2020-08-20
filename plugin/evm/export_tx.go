// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"

	safemath "github.com/ava-labs/gecko/utils/math"
)

var (
	errNoExportOutputs  = errors.New("no export outputs")
	errOutputsNotSorted = errors.New("outputs not sorted")
	errOverflowExport   = errors.New("overflow when computing export amount + txFee")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	avax.Metadata
	// true iff this transaction has already passed syntactic verification
	syntacticallyVerified bool
	// ID of the network on which this tx was issued
	NetworkID uint32 `serialize:"true" json:"networkID"`
	// ID of this blockchain.
	BlockchainID ids.ID `serialize:"true" json:"blockchainID"`
	// Which chain to send the funds to
	DestinationChain ids.ID `serialize:"true" json:"destinationChain"`
	// Inputs
	Ins []EVMInput `serialize:"true" json:"inputs"`
	// Outputs that are exported to the chain
	ExportedOutputs []*avax.TransferableOutput `serialize:"true" json:"exportedOutputs"`
}

// InputUTXOs returns an empty set
func (tx *UnsignedExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// Verify this transaction is well-formed
func (tx *UnsignedExportTx) Verify(
	avmID ids.ID,
	ctx *snow.Context,
	feeAmount uint64,
	feeAssetID ids.ID,
) error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.DestinationChain.IsZero():
		return errWrongChainID
	case !tx.DestinationChain.Equals(avmID):
		return errWrongChainID
	case len(tx.ExportedOutputs) == 0:
		return errNoExportOutputs
	case tx.NetworkID != ctx.NetworkID:
		return errWrongNetworkID
	case !ctx.ChainID.Equals(tx.BlockchainID):
		return errWrongBlockchainID
	}

	for _, in := range tx.Ins {
		if err := in.Verify(); err != nil {
			return err
		}
	}

	for _, out := range tx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !avax.IsSortedTransferableOutputs(tx.ExportedOutputs, Codec) {
		return errOutputsNotSorted
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedExportTx) SemanticVerify(
	vm *VM,
	stx *Tx,
) TxError {
	if err := tx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID); err != nil {
		return permError{err}
	}

	// TODO: credential check
	// TODO: flow check
	// TODO: verify UTXO outputs via gRPC
	return nil
}

// Accept this transaction.
func (tx *UnsignedExportTx) Accept(ctx *snow.Context, batch database.Batch) error {
	// TODO: finish this function via gRPC
	return nil
}

// Create a new transaction
func (vm *VM) newExportTx(
	amount uint64, // Amount of tokens to export
	chainID ids.ID, // Chain to send the UTXOs to
	to ids.ShortID, // Address of chain recipient
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
) (*Tx, error) {
	if !vm.ctx.XChainID.Equals(chainID) {
		return nil, errWrongChainID
	}

	toBurn, err := safemath.Add64(amount, vm.txFee)
	if err != nil {
		return nil, errOverflowExport
	}
	ins, signers, err := vm.GetSpendableCanonical(keys, toBurn)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	utx := &UnsignedExportTx{
		NetworkID:        vm.ctx.NetworkID,
		BlockchainID:     vm.ctx.ChainID,
		DestinationChain: chainID,
		Ins:              ins,
		ExportedOutputs: []*avax.TransferableOutput{{ // Exported to X-Chain
			Asset: avax.Asset{ID: vm.ctx.AVAXAssetID},
			Out: &secp256k1fx.TransferOutput{
				Amt: amount,
				OutputOwners: secp256k1fx.OutputOwners{
					Locktime:  0,
					Threshold: 1,
					Addrs:     []ids.ShortID{to},
				},
			},
		}},
	}
	tx := &Tx{UnsignedTx: utx}
	if err := tx.Sign(vm.codec, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify(vm.ctx.XChainID, vm.ctx, vm.txFee, vm.ctx.AVAXAssetID)
}
