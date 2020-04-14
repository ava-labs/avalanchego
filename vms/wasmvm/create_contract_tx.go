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
	ID ids.ID

	// Byte rept. of the transaction
	WasmBytes []byte `serialize:"true"`

	// Byte repr. of this tx
	bytes []byte
}

// Bytes returns the byte representation of this transaction
func (tx *createContractTx) Bytes() []byte {
	return tx.bytes
}

// should be called when unmarshaling
func (tx *createContractTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't initialize tx: %v", err)
	}
	tx.ID = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

// SyntacticVerify returns nil iff tx is syntactically valid
func (tx *createContractTx) SyntacticVerify() error {
	switch {
	case tx.WasmBytes == nil:
		return fmt.Errorf("empty contract")
	case tx.ID.Equals(ids.Empty):
		return fmt.Errorf("empty tx ID")
	}
	return nil
}

func (tx *createContractTx) SemanticVerify(database.Database) error {
	return nil // TODO
}

func (tx *createContractTx) Accept() {
	tx.vm.Ctx.Log.Debug("creating contract %s", tx.ID) // TODO delete
	if err := tx.vm.putContractBytes(tx.vm.DB, tx.ID, tx.WasmBytes); err != nil {
		tx.vm.Ctx.Log.Error("couldn't put new contract in db: %v", err)
	}
	if err := tx.vm.putContractState(tx.vm.DB, tx.ID, []byte{}); err != nil {
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
