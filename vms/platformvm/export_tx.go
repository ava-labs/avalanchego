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
	"github.com/ava-labs/gecko/utils/math"
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

	// ID of this tx
	id ids.ID

	// Byte representation of the unsigned transaction
	unsignedBytes []byte

	// Byte representation of the signed transaction (ie with Creds and ControlSigs)
	bytes []byte

	// ID of the network this blockchain exists on
	NetworkID uint32 `serialize:"true"`

	// Input UTXOs
	Ins []*ava.TransferableInput `serialize:"true"`

	// Output UTXOs
	Outs []*ava.TransferableOutput `serialize:"true"`
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
	tx.id = ids.NewID(hashing.ComputeHash256Array(txBytes))
	return err
}

// ID of this transaction
func (tx *ExportTx) ID() ids.ID { return tx.id }

// Key returns the public key of the signer of this transaction
// Precondition: tx.Verify() has been called and returned nil
func (tx *ExportTx) Key() crypto.PublicKey { return tx.key }

// Bytes returns the byte representation of an ExportTx
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
		if !out.AssetID().Equals(tx.vm.ava) {
			return errUnknownAsset
		}
	}
	if !ava.IsSortedTransferableOutputs(tx.Outs, Codec) {
		return errOutputsNotSorted
	}

	unsignedIntf := interface{}(&tx.UnsignedExportTx)
	unsignedBytes, err := Codec.Marshal(&unsignedIntf) // byte repr of unsigned tx
	if err != nil {
		return err
	}

	key, err := tx.vm.factory.RecoverPublicKey(unsignedBytes, tx.Sig[:])
	if err != nil {
		return err
	}

	tx.key = key
	return nil
}

// SemanticVerify this transaction is valid.
func (tx *ExportTx) SemanticVerify(db database.Database) error {
	if err := tx.SyntacticVerify(); err != nil {
		return err
	}

	amount := uint64(0)
	for _, out := range tx.Outs {
		newAmount, err := math.Add64(out.Out.Amount(), amount)
		if err != nil {
			return err
		}
		amount = newAmount
	}

	accountID := tx.key.Address()
	account, err := tx.vm.getAccount(db, accountID)
	if err != nil {
		return errDBAccount
	}

	account, err = account.Remove(amount, tx.Nonce)
	if err != nil {
		return err
	}
	return tx.vm.putAccount(db, account)
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
