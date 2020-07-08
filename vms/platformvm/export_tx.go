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
	safemath "github.com/ava-labs/gecko/utils/math"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

var (
	errNoExportOutputs  = errors.New("no export outputs")
	errOutputsNotSorted = errors.New("outputs not sorted")
	errOverflowExport   = errors.New("overflow when computing export amount + txFee")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	// Metadata, inputs and outputs
	// The outputs in CommonTx are non-exported
	CommonTx `serialize:"true"`
	// Amount of nAVAX being exported to the X-Chain
	Amount uint64 `serialize:"true"`
	// Outputs that are exported to the X-Chain
	// Should only have one element
	ExportedOutputs []*ava.TransferableOutput `serialize:"true"`
}

// ExportTx exports funds to the AVM
type ExportTx struct {
	UnsignedExportTx `serialize:"true"`
	// Credentials that authorize the inputs to spend the corresponding outputs
	Credentials []verify.Verifiable `serialize:"true"`
}

// Outs returns this transaction's outputs
func (tx *ExportTx) Outs() []*ava.TransferableOutput {
	outs := tx.CommonTx.Outs()
	outs = append(outs, tx.ExportedOutputs...)
	return outs
}

// InputUTXOs returns the IDs of the UTXOs this tx consumes
func (tx *ExportTx) InputUTXOs() ids.Set {
	set := ids.Set{}
	for _, in := range tx.Ins() {
		set.Add(in.InputID())
	}
	return set
}

// Creds returns this transactions credentials
func (tx *ExportTx) Creds() []verify.Verifiable {
	return tx.Credentials
}

// initialize [tx]. Sets [tx.vm], [tx.unsignedBytes], [tx.bytes], [tx.id]
func (tx *ExportTx) initialize(vm *VM) error {
	if tx.vm != nil { // already been initialized
		return nil
	}
	tx.vm = vm
	var err error
	tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedExportTx))
	if err != nil {
		return fmt.Errorf("couldn't marshal UnsignedExportTx: %w", err)
	}
	tx.bytes, err = Codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal ExportTx: %w", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return err
}

// SyntacticVerify this transaction is well-formed
func (tx *ExportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.syntacticallyVerified: // already passed syntactic verification
		return nil
	case tx.NetworkID != tx.vm.Ctx.NetworkID:
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case len(tx.ExportedOutputs) != 1:
		return fmt.Errorf("tx has %d exported outputs but should have 1", len(tx.ExportedOutputs))
	}
	if exportedAmt := tx.ExportedOutputs[0].Output().Amount(); exportedAmt != tx.Amount {
		return fmt.Errorf("exported output has amount %d but should be %d", exportedAmt, tx.Amount)
	}
	toSpend, err := safemath.Add64(tx.Amount, tx.vm.txFee)
	if err != nil {
		return errOverflowExport
	}
	if err := syntacticVerifySpend(tx, toSpend, tx.vm.avaxAssetID); err != nil {
		return err
	}
	tx.syntacticallyVerified = true
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ExportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}
	// Spend inputs
	for index, in := range tx.Inputs {
		if utxo, err := tx.vm.getUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		} else if err := tx.vm.fx.VerifyTransfer(tx, in.In, tx.Credentials[index], utxo.Out); err != nil {
			return err
		} else if err := tx.vm.removeUTXO(db, in.UTXOID.InputID()); err != nil {
			return err
		}
	}
	// Produce non-exported UTXOs
	txID := tx.ID()
	for index, out := range tx.CommonTx.Outputs {
		if err := tx.vm.putUTXO(db, &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(index),
			},
			Asset: ava.Asset{ID: tx.vm.avaxAssetID},
			Out:   out.Output(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// Accept this transaction.
func (tx *ExportTx) Accept(batch database.Batch) error {

	// Produce exported UTXOs
	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)
	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, Codec)
	utxo := &ava.UTXO{
		UTXOID: ava.UTXOID{
			TxID:        tx.ID(),
			OutputIndex: uint32(len(tx.CommonTx.Outputs)),
		},
		Asset: ava.Asset{ID: tx.vm.avaxAssetID},
		Out:   tx.ExportedOutputs[0].Output(), // SyntacticVerify guarantees len(ExportedOutput) == 1
	}
	if err := state.FundPlatformUTXO(utxo); err != nil {
		return err
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
) (*ExportTx, error) {

	var err error
	amount, err = safemath.Add64(amount, vm.txFee)
	if err != nil {
		return nil, errOverflowExport
	}

	// Calculate inputs, outputs, and keys used to sign this tx
	// Burn the tx fee and the amount being sent to the X-Chain
	ins, outs, credKeys, err := vm.spend(vm.DB, amount, keys)
	if err != nil {
		return nil, fmt.Errorf("couldn't generate tx inputs/outputs: %w", err)
	}

	// Create the transaction
	tx := &ExportTx{
		UnsignedExportTx: UnsignedExportTx{
			CommonTx: CommonTx{
				NetworkID:    vm.Ctx.NetworkID,
				BlockchainID: ids.Empty,
				Inputs:       ins,
				Outputs:      outs, // Non-exported outputs
			},
			ExportedOutputs: []*ava.TransferableOutput{ // Exported to X-Chain
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: vm.avaxAssetID},
					Out: &secp256k1fx.TransferOutput{
						Amt:      amount,
						Locktime: 0,
						OutputOwners: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{to},
						},
					},
				},
			},
		},
	}

	// Generate byte repr. of unsigned transaction
	if tx.unsignedBytes, err = Codec.Marshal(interface{}(tx.UnsignedExportTx)); err != nil {
		return nil, fmt.Errorf("couldn't marshal UnsignedExportTx: %w", err)
	}
	hash := hashing.ComputeHash256(tx.unsignedBytes)

	// Attach credentials that allow the inputs to be spent
	for _, inputKeys := range credKeys { // [inputKeys] are the keys used to authorize spend of an input
		cred := &secp256k1fx.Credential{}
		for _, key := range inputKeys {
			sig, err := key.SignHash(hash) // Sign hash(tx.unsignedBytes)
			if err != nil {
				return nil, fmt.Errorf("problem generating credential: %w", err)
			}
			sigArr := [crypto.SECP256K1RSigLen]byte{}
			copy(sigArr[:], sig)
			cred.Sigs = append(cred.Sigs, sigArr)
		}
		tx.Credentials = append(tx.Credentials, cred) // Attach credential to tx
	}

	return tx, tx.initialize(vm)
}
