// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/chains/atomic"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
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
	BaseTx `serialize:"true"`
	// Outputs that are exported to the X-Chain
	ExportedOutputs []*ava.TransferableOutput `serialize:"true"`
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *UnsignedExportTx) initialize(vm *VM, bytes []byte) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	tx.bytes = bytes
	tx.id = ids.NewID(hashing.ComputeHash256Array(bytes))
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedExportTx: %w", err)
	}
	return nil
}

// InputUTXOs returns an empty set
func (tx *UnsignedExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// Verify this transaction is well-formed
func (tx *UnsignedExportTx) Verify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case len(tx.ExportedOutputs) == 0:
		return errNoExportOutputs
	}

	if err := tx.BaseTx.Verify(); err != nil {
		return err
	}

	for _, out := range tx.ExportedOutputs {
		if err := out.Verify(); err != nil {
			return err
		}
	}
	if !ava.IsSortedTransferableOutputs(tx.ExportedOutputs, Codec) {
		return errOutputsNotSorted
	}

	allOuts := make([]*ava.TransferableOutput, len(tx.Outs)+len(tx.ExportedOutputs))
	copy(allOuts, tx.Outs)
	copy(allOuts[len(tx.Outs):], tx.ExportedOutputs)
	if err := syntacticVerifySpend(tx.Ins, allOuts, tx.vm.txFee, tx.vm.avaxAssetID); err != nil {
		return err
	}

	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *UnsignedExportTx) SemanticVerify(db database.Database, creds []verify.Verifiable) TxError {
	if err := tx.Verify(); err != nil {
		return permError{err}
	}
	return tx.vm.semanticVerifySpend(db, tx, tx.Ins, tx.Outs, creds)
}

// Accept this transaction.
func (tx *UnsignedExportTx) Accept(batch database.Batch) error {
	// Produce exported UTXOs
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	vsmDB := versiondb.New(smDB)
	state := ava.NewPrefixedState(vsmDB, Codec)

	txID := tx.ID()
	for i, out := range tx.ExportedOutputs {
		if err := state.FundPlatformUTXO(&ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(len(tx.BaseTx.Outs) + i),
			},
			Asset: out.Asset,
			Out:   out.Output(),
		}); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}
	return atomic.WriteAll(batch, sharedBatch)
}

// Create a new transaction
func (vm *VM) newExportTx(
	amount uint64, // Amount of tokens to export
	to ids.ShortID, // Address of X-Chain recipient
	keys []*crypto.PrivateKeySECP256K1R, // Pay the fee and provide the tokens
) (*AtomicTx, error) {
	toBurn, err := safemath.Add64(amount, vm.txFee)
	if err != nil {
		return nil, errOverflowExport
	}
	ins, outs, signers, err := vm.burn(vm.DB, keys, toBurn, 0)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	utx := &UnsignedExportTx{
		BaseTx: BaseTx{
			NetworkID:    vm.Ctx.NetworkID,
			BlockchainID: vm.Ctx.ChainID,
			Ins:          ins,
			Outs:         outs, // Non-exported outputs
		},
		ExportedOutputs: []*ava.TransferableOutput{{ // Exported to X-Chain
			Asset: ava.Asset{ID: vm.avaxAssetID},
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
	tx := &AtomicTx{UnsignedAtomicTx: utx}
	if err := vm.signAtomicTx(tx, signers); err != nil {
		return nil, err
	}
	return tx, utx.Verify()
}
