package wasmvm

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/gecko/utils/formatting"

	"github.com/wasmerio/go-ext-wasm/wasmer"

	"github.com/ava-labs/gecko/snow/choices"

	"github.com/ava-labs/gecko/database/prefixdb"

	"github.com/ava-labs/gecko/utils/hashing"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

// A SC's return value is mapped to by this key in the SC's database
var returnKey = []byte{1}

// invokes a function of a contract
type invokeTx struct {
	vm    *VM
	bytes []byte

	// ID of this tx
	id ids.ID

	// ID of contract to invoke
	ContractID ids.ID `serialize:"true"`

	// Name of function to invoke
	FunctionName string `serialize:"true"`

	// Arguments to the function
	Arguments []interface{} `serialize:"true"`

	// Byte arguments to pass to the method
	// Should be in the form of a JSON
	ByteArguments []byte `serialize:"true"`
}

// ID returns this tx's ID
// Should only be called after initialize
func (tx *invokeTx) ID() ids.ID {
	return tx.id
}

func (tx *invokeTx) SyntacticVerify() error {
	switch {
	case tx.id.Equals(ids.Empty):
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

// SemanticVerify ensures the state transition of this tx is valid.
// It writes state changes to [db].
// [db] will only be comitted (actually change the chain's state) if this method returns nil.
// This method must set the contract's context before invoking the SC method.
//
// Byte arguments to the SC method are mapped to by the empty byte array (ie []byte{}) in the SC's database.
//
// A SC method has two ways to return information to the chain.
// The first is the literal return value of the method. A return value of 0 indicates the SC method
// executed successfully. Any other return value indicates failure.
// All SC method's must follow this convention.
//
// The other way is for the SC to create a KV pair in its database where the key is a byte array
// containing only 1 (ie []byte{1}) and the value is the return value of the method.
// A SC method need not do this. Such a method will be considered to have returned "void".
func (tx *invokeTx) SemanticVerify(db database.Database) error {
	// Get the contract and its state
	contract, err := tx.vm.getContract(db, tx.ContractID)
	if err != nil {
		return fmt.Errorf("couldn't load contract %s: %s", tx.ContractID, err)
	}

	// Prefixed database for the contract to read/write
	// TODO: Find a way to do this without creating a new prefixdb with every invocation
	prefix := tx.ContractID.Key()
	contractDb := prefixdb.New(prefix[:], db)

	// Update the contract's context
	contract.SetContextData(ctx{
		log:    tx.vm.Ctx.Log,
		db:     contractDb,
		memory: contract.Memory,
		txID:   tx.ID(),
	})

	// Get the function to call
	fn, exists := contract.Exports[tx.FunctionName]
	if !exists {
		return fmt.Errorf("contract has no function '%s'", tx.FunctionName)
	}

	// Set the byteArguments to pass to function
	// They're mapped to by the empty key in the contract's db
	if err := contractDb.Put([]byte{}, tx.ByteArguments); err != nil {
		return fmt.Errorf("couldn't set byte arguments: %v", err)
	}

	// Clear the old return value
	db.Delete(returnKey)

	// Call the function
	val, err := fn(tx.Arguments...)
	if err != nil {
		return fmt.Errorf("error during call to function '%s': %v", tx.FunctionName, err)
	}

	var success bool
	switch val.GetType() {
	case wasmer.TypeI32:
		success = val.ToI32() == int32(0)
	case wasmer.TypeI64:
		success = val.ToI64() == int64(0)
	default:
		return fmt.Errorf("smart contract method must return int32 or int64")
	}

	tx.vm.Ctx.Log.Info("call to '%s' returned: %v", tx.FunctionName, val)
	val.GetType()

	// Save the contract's state
	if err := tx.vm.putContractState(db, tx.ContractID, contract.Memory.Data()); err != nil {
		return fmt.Errorf("couldn't save contract's state: %v", err)
	}

	// Persist the transaction and its return value
	returnValue := []byte{}
	returnValue, _ = contractDb.Get(returnKey)
	rv := &txReturnValue{ // TODO: persist tx in every execution of this method
		Tx:                   tx,
		Status:               choices.Accepted,
		InvocationSuccessful: success,
		ReturnValue:          returnValue,
	}
	if err := tx.vm.putTx(db, rv); err != nil {
		return fmt.Errorf("couldn't persist transaction: %v", err)
	}

	return nil
}

// Set tx.vm, tx.bytes, tx.id
func (tx *invokeTx) initialize(vm *VM) error {
	tx.vm = vm
	var err error
	tx.bytes, err = codec.Marshal(tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal invokeTx: %v", err)
	}
	tx.id = ids.NewID(hashing.ComputeHash256Array(tx.bytes))
	return nil
}

// Creates a new, initialized tx
func (vm *VM) newInvokeTx(contractID ids.ID, functionName string, args []interface{}, byteArgs []byte) (*invokeTx, error) {
	tx := &invokeTx{
		vm:            vm,
		ContractID:    contractID,
		FunctionName:  functionName,
		Arguments:     args,
		ByteArguments: byteArgs,
	}
	if err := tx.initialize(vm); err != nil {
		return nil, err
	}
	return tx, nil
}

func (tx *invokeTx) MarshalJSON() ([]byte, error) {
	asMap := make(map[string]interface{}, 4)
	asMap["contractID"] = tx.ContractID.String()
	asMap["function"] = tx.FunctionName
	asMap["arguments"] = tx.Arguments
	byteArgs := formatting.CB58{Bytes: tx.ByteArguments}
	asMap["byteArguments"] = byteArgs.String()
	return json.Marshal(asMap)
}
