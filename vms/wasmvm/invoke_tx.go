package wasmvm

import (
	"crypto/rand"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

// ArgType is the type of an argument to a wasm file
type ArgType uint8

const (
	Int ArgType = iota
	String
)

// Argument to a function in wasm
type Argument struct {
	Type  ArgType     `serialize:"true"`
	Value interface{} `serialize:"true"`
}

// invokes a function of a contract
type invokeTx struct {
	vm    *VM
	bytes []byte

	// ID of this tx
	ID ids.ID `serialize:"true"`

	// ID of contract to invoke
	ContractID ids.ID `serialize:"true"`

	// Name of function to invoke
	FunctionName string `serialize:"true"`

	// Arguments to the function
	Arguments []Argument `serialize:"true"`
}

func (tx *invokeTx) SyntacticVerify() error {
	return nil // TODO
}

func (tx *invokeTx) SemanticVerify(database.Database) error {
	return nil // TODO
}

func (tx *invokeTx) Accept() {
	// TODO
}

func (tx *invokeTx) initialize(vm *VM) {
	tx.vm = vm
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't initialize tx: %v", err)
	}
}

// Creates a new, initialized tx
func (vm *VM) newInvokeTx(contractID ids.ID, functionName string, args []Argument) (*invokeTx, error) {
	var idBytes [32]byte
	if n, err := rand.Read(idBytes[:32]); err != nil {
		return nil, fmt.Errorf("couldn't generate new ID: %s", err)
	} else if n != 32 {
		return nil, fmt.Errorf("ID should be 32 bytes but is %d bytes", n)
	}
	tx := &invokeTx{
		vm:           vm,
		ID:           ids.NewID(idBytes),
		ContractID:   contractID,
		FunctionName: functionName,
		Arguments:    args,
	}
	tx.initialize(vm)
	return tx, nil
}
