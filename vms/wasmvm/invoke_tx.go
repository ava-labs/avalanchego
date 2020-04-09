package wasmvm

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

const bytesPerPage = 65 * 1024 // according to go-ext-wasm

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
	Arguments []interface{} `serialize:"true"`
}

func (tx *invokeTx) SyntacticVerify() error {
	switch {
	case tx.ID.Equals(ids.Empty):
		return errors.New("tx ID is empty")
	case tx.FunctionName == "":
		return errors.New("function name is empty")
	}
	return nil
}

func (tx *invokeTx) SemanticVerify(database.Database) error {
	return nil // TODO
}

func (tx *invokeTx) Accept() {
	// TODO: Move most of this to semanticVerify

	// Get the contract's bytes
	contractBytes, err := tx.vm.getContract(tx.vm.DB, tx.ContractID)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't get contract %s", tx.ContractID, err)
		return
	}

	// Parse contract to from bytes
	imports := standardImports()
	contract, err := wasm.NewInstanceWithImports(contractBytes, imports)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't instantiate contract: %v", err)
		return
	}
	defer contract.Close()

	// Set the contract's state to be what it was after last call
	state, err := tx.vm.getContractState(tx.vm.DB, tx.ContractID)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't get contract's state: %v", err)
		return
	}
	memory := contract.Memory // The contract's memory

	if needMoreMemory := uint32(len(state)) > memory.Length(); needMoreMemory {
		additionalBytesNeeded := uint32(len(state)) - memory.Length()
		additionalPagesNeeded := (additionalBytesNeeded / bytesPerPage) + 1 //round up
		if err := memory.Grow(uint32(additionalPagesNeeded)); err != nil {
			tx.vm.Ctx.Log.Error("couldn't grow contract's state: %v", err)
			return
		}
	}
	copy(memory.Data(), state) // Copy the state over

	// Get the function to call
	fn, exists := contract.Exports[tx.FunctionName]
	if !exists {
		tx.vm.Ctx.Log.Error("contract has no function '%s'", tx.FunctionName)
		return
	}

	// Call the function
	val, err := fn(tx.Arguments...)
	if err != nil {
		tx.vm.Ctx.Log.Error("error during call to function '%s': %v", tx.FunctionName, err)
	}

	fmt.Printf("call to '%s' returned: %v\n", tx.FunctionName, val) // TODO how to get returned values out?

	// Save the contract's state
	state = contract.Memory.Data()
	if err := tx.vm.putContractState(tx.vm.DB, tx.ContractID, state); err != nil {
		tx.vm.Ctx.Log.Error("couldn't save contract's state: %v", err)
	}
}

func (tx *invokeTx) initialize(vm *VM) {
	tx.vm = vm
	tx.vm.Ctx.Log.Verbo("initializing tx: %+v", tx)
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't initialize tx: %v", err)
	}
}

// Creates a new, initialized tx
func (vm *VM) newInvokeTx(contractID ids.ID, functionName string, args []interface{}) (*invokeTx, error) {
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
