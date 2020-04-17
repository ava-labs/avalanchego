package wasmvm

import (
	"fmt"

	"github.com/ava-labs/gecko/utils/hashing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

// Creates a contract
type createContractTx struct {
	vm *VM

	// ID of this tx and the contract being created
	id ids.ID

	// Byte rept. of the transaction
	WasmBytes []byte `serialize:"true"`

	// Byte repr. of this tx
	bytes []byte
}

// Bytes returns the byte representation of this transaction
func (tx *createContractTx) Bytes() []byte {
	return tx.bytes
}

// ID returns this tx's ID
// Should only be called after tx is initialized
func (tx *createContractTx) ID() ids.ID {
	return tx.id
}

// should be called when unmarshaling
func (tx *createContractTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't initialize tx: %v", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

// SyntacticVerify returns nil iff tx is syntactically valid
func (tx *createContractTx) SyntacticVerify() error {
	switch {
	case tx.WasmBytes == nil:
		return fmt.Errorf("empty contract")
	case tx.id.Equals(ids.Empty):
		return fmt.Errorf("empty tx ID")
	}
	return nil
}

func (tx *createContractTx) SemanticVerify(database.Database) error {
	return nil // TODO
}

func (tx *createContractTx) Accept() {
	tx.vm.Ctx.Log.Debug("creating contract %s", tx.id) // TODO delete
	if err := tx.vm.putContractBytes(tx.vm.DB, tx.id, tx.WasmBytes); err != nil {
		tx.vm.Ctx.Log.Error("couldn't put new contract in db: %v", err)
	}
	if err := tx.vm.putContractState(tx.vm.DB, tx.id, []byte{}); err != nil {
		tx.vm.Ctx.Log.Error("couldn't initialize contract's state in db: %v", err)
	}
}

// Creates a new tx with the given payload and a random ID
func (vm *VM) newCreateContractTx(wasmBytes []byte) (*createContractTx, error) {
	tx := &createContractTx{
		vm:        vm,
		WasmBytes: wasmBytes,
	}
	if err := tx.initialize(vm); err != nil {
		return nil, err
	}
	return tx, nil
}
