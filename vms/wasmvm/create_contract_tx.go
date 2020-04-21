package wasmvm

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/gecko/snow/choices"

	"github.com/ava-labs/gecko/utils/formatting"

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
		/* TODO: Put back this check once we stop hard-coding contracts
		case tx.id.Equals(ids.Empty):
			return fmt.Errorf("empty tx ID")
		*/
	}
	return nil
}

func (tx *createContractTx) SemanticVerify(db database.Database) error {
	if err := tx.vm.putContractBytes(db, tx.id, tx.WasmBytes); err != nil {
		return fmt.Errorf("couldn't put new contract in db: %v", err)
	}
	if err := tx.vm.putContractState(db, tx.id, []byte{}); err != nil {
		return fmt.Errorf("couldn't initialize contract's state in db: %v", err)
	}
	persistedTx := &txReturnValue{ // TODO: always persist the tx, even if it was unsuccessful
		Tx:     tx,
		Status: choices.Accepted,
	}
	if err := tx.vm.putTx(db, persistedTx); err != nil {
		return err
	}
	return nil
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

func (tx *createContractTx) MarshalJSON() ([]byte, error) {
	asMap := make(map[string]interface{}, 2)
	asMap["id"] = tx.ID().String()
	byteFormatter := formatting.CB58{Bytes: tx.WasmBytes}
	asMap["contract"] = byteFormatter.String()
	return json.Marshal(asMap)
}
