package wasmvm

import (
	"crypto/rand"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

// Creates a contract
type createContractTx struct {
	vm *VM

	// ID of this tx
	ID ids.ID `serialize:"true"`

	// Byte rept. of the transaction
	WasmBytes []byte `serialize:"true"`

	// Byte repr. of this tx
	bytes []byte
}

// Bytes returns the byte representation of this transaction
func (tx *createContractTx) Bytes() []byte {
	return tx.bytes
}

func (tx *createContractTx) initialize(vm *VM) {
	tx.vm = vm
}

// SyntacticVerify returns nil iff tx is syntactically valid
func (tx *createContractTx) SyntacticVerify() error {
	switch {
	case tx.WasmBytes == nil:
		return fmt.Errorf("empty WasmBytes")
	case tx.ID.Equals(ids.Empty):
		return fmt.Errorf("empty tx ID")
	}
	return nil
}

func (tx *createContractTx) SemanticVerify(database.Database) error {
	return nil
}

func (tx *createContractTx) Accept() {
	if err := tx.contract.Invoke(tx.Payload, tx.contractDB); err != nil {
		tx.vm.Ctx.Log.Error("error during invoke: %s", err)
	}
}

// Creates a new tx with the given payload and a random ID
func (vm *VM) newCreateContractTx(contractID ids.ID, payload []byte) (*createContractTx, error) {
	var idBytes [32]byte
	if n, err := rand.Read(idBytes[:32]); err != nil {
		return nil, fmt.Errorf("couldn't generate new ID: %s", err)
	} else if n != 32 {
		return nil, fmt.Errorf("id should be 32 bytes but is %d bytes", n)
	}
	return &createContractTx{
		vm:         vm,
		ID:         ids.NewID(idBytes),
		Payload:    payload,
		ContractID: contractID,
	}, nil
}
