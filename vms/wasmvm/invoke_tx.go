package wasmvm

import (
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/utils/hashing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

const bytesPerPage = 65 * 1024 // according to go-ext-wasm

// invokes a function of a contract
type invokeTx struct {
	vm    *VM
	bytes []byte

	// ID of this tx
	ID ids.ID

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

	// Ensure all arguments are floats or ints
	for _, arg := range tx.Arguments {
		switch argType := arg.(type) {
		case int32, int64, float32, float64:
		default:
			return fmt.Errorf("an argument has type %v. Must be one of: int32, int64, float32, float64", argType)
		}
	}
	// TODO add more validation
	return nil
}

func (tx *invokeTx) SemanticVerify(database.Database) error {
	return nil // TODO
}

func (tx *invokeTx) Accept() {
	// TODO: Move most of this to semanticVerify

	// Get the contract. Its state is also loaded.
	contract, err := tx.vm.getContract(tx.vm.DB, tx.ContractID)
	if err != nil {
		tx.vm.Ctx.Log.Error("couldn't load contract %s: %s", tx.ContractID, err)
		return
	}

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

	tx.vm.Ctx.Log.Info("call to '%s' returned: %v", tx.FunctionName, val) // TODO how to get returned values out?

	// Save the contract's state
	if err := tx.vm.putContractState(tx.vm.DB, tx.ContractID, contract.Memory.Data()); err != nil {
		tx.vm.Ctx.Log.Error("couldn't save contract's state: %v", err)
	}
}

// Set tx.vm, tx.bytes, tx.id
func (tx *invokeTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal invokeTx: %v", err)
	}
	tx.ID = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

// Creates a new, initialized tx
func (vm *VM) newInvokeTx(contractID ids.ID, functionName string, args []interface{}) (*invokeTx, error) {
	tx := &invokeTx{
		vm:           vm,
		ContractID:   contractID,
		FunctionName: functionName,
		Arguments:    args,
	}
	if err := tx.initialize(vm); err != nil {
		return nil, err
	}
	return tx, nil
}
