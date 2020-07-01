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
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
)

var (
	errNoExportOutputs  = errors.New("no export outputs")
	errOutputsNotSorted = errors.New("outputs not sorted")
)

// UnsignedExportTx is an unsigned ExportTx
type UnsignedExportTx struct {
	vm *VM

	// Metadata, inputs and outputs
	CommonTx `serialize:"true"`
}

// UnsignedBytes returns the byte representation of this unsigned tx
func (tx *UnsignedExportTx) UnsignedBytes() []byte {
	return tx.unsignedBytes
}

// ExportTx exports funds to the AVM
type ExportTx struct {
	UnsignedExportTx `serialize:"true"`

	// Credentials that authorize the inputs to spend the corresponding outputs
	Creds []verify.Verifiable `serialize:"true"`
}

// initialize [tx]
// set tx.vm, tx.unsignedBytes, tx.bytes, tx.id
func (tx *ExportTx) initialize(vm *VM) error {
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

// ID of this transaction
func (tx *ExportTx) ID() ids.ID { return tx.id }

// Bytes returns the byte representation of an ExportTx
// Should only be called after initialize
func (tx *ExportTx) Bytes() []byte { return tx.bytes }

// InputUTXOs returns an empty set
func (tx *ExportTx) InputUTXOs() ids.Set { return ids.Set{} }

// SyntacticVerify this transaction is well-formed
// TODO only syntacticVerify once
func (tx *ExportTx) SyntacticVerify() error {
	switch {
	case tx == nil:
		return errNilTx
	case tx.NetworkID != tx.vm.Ctx.NetworkID: // verify the transaction is on this network
		return errWrongNetworkID
	case tx.id.IsZero():
		return errInvalidID
	case len(tx.Outs) == 0:
		return errNoExportOutputs
	}

	for _, out := range tx.Outs {
		if err := out.Verify(); err != nil {
			return err
		}
		if !out.AssetID().Equals(tx.vm.avaxAssetID) {
			return errUnknownAsset
		}
	}
	if !ava.IsSortedTransferableOutputs(tx.Outs, Codec) {
		return errOutputsNotSorted
	}
	return nil
}

// SemanticVerify this transaction is valid.
// TODO make sure the ins and outs are semantically valid
func (tx *ExportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	// Update the UTXO set
	for _, in := range tx.Ins {
		utxoID := in.InputID() // ID of the UTXO that [in] spends
		if err := tx.vm.removeUTXO(db, utxoID); err != nil {
			return tempError{fmt.Errorf("couldn't remove UTXO %s from UTXO set: %w", utxoID, err)}
		}
	}
	for _, out := range tx.Outs {
		if err := tx.vm.putUTXO(db, tx.ID(), out); err != nil {
			return tempError{fmt.Errorf("couldn't add UTXO to UTXO set: %w", err)}
		}
	}
	return nil
}

// Accept this transaction.
func (tx *ExportTx) Accept(batch database.Batch) error {
	txID := tx.ID()

	smDB := tx.vm.Ctx.SharedMemory.GetDatabase(tx.vm.avm)
	defer tx.vm.Ctx.SharedMemory.ReleaseDatabase(tx.vm.avm)

	vsmDB := versiondb.New(smDB)

	state := ava.NewPrefixedState(vsmDB, Codec)
	for i, out := range tx.Outs {
		utxo := &ava.UTXO{
			UTXOID: ava.UTXOID{
				TxID:        txID,
				OutputIndex: uint32(i),
			},
			Asset: ava.Asset{ID: out.AssetID()},
			Out:   out.Out,
		}
		if err := state.FundPlatformUTXO(utxo); err != nil {
			return err
		}
	}

	sharedBatch, err := vsmDB.CommitBatch()
	if err != nil {
		return err
	}

	return atomic.WriteAll(batch, sharedBatch)
}

/* TODO implement
func (vm *VM) newExportTx(nonce uint64, networkID uint32, outs []*ava.TransferableOutput, from *crypto.PrivateKeySECP256K1R) (*ExportTx, error) {
	ava.SortTransferableOutputs(outs, Codec)

	tx := &ExportTx{UnsignedExportTx: UnsignedExportTx{
		NetworkID: networkID,
		Nonce:     nonce,
		Outs:      outs,
	}}

	unsignedIntf := interface{}(&tx.UnsignedExportTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // Byte repr. of unsigned transaction
	if err != nil {
		return nil, err
	}

	sig, err := from.Sign(unsignedBytes)
	if err != nil {
		return nil, err
	}
	copy(tx.Sig[:], sig)

	return tx, tx.initialize(vm)
}
*/
