package wasmvm

import (
	"fmt"

	"github.com/ava-labs/gecko/snow/choices"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	wasm "github.com/wasmerio/go-ext-wasm/wasmer"
)

const (
	contractBytesTypeID uint64 = iota
	stateTypeID
	returnValueTypeID
)

// put a contract (in its raw byte form) in the database
func (vm *VM) putContractBytes(db database.Database, ID ids.ID, contract []byte) error {
	return vm.State.Put(db, contractBytesTypeID, ID, bytes{contract})
}

// get a contract (in its raw byte form) by its ID
func (vm *VM) getContractBytes(db database.Database, ID ids.ID) ([]byte, error) {
	contractIntf, err := vm.State.Get(db, contractBytesTypeID, ID)
	if err != nil {
		return nil, err
	}
	return contractIntf.([]byte), nil
}

// get the contract with the given ID.
func (vm *VM) getContract(db database.Database, ID ids.ID) (*wasm.Instance, error) {
	// Check the cache for the contract
	var contract *wasm.Instance
	contractIntf, ok := vm.contracts.Get(ID)
	if ok { // It is in the cache
		contract, ok = contractIntf.(*wasm.Instance)
		if ok {
			return contract, nil
		}
		vm.Ctx.Log.Error("expected *wasm.Instance from cache but got another type...will try to parse contract from bytes")
	}
	// It is not in the cache
	// Get the contract's byte repr.
	contractBytes, err := vm.getContractBytes(db, ID)
	if err != nil {
		return nil, fmt.Errorf("couldn't find contract %s", ID)
	}

	// Parse contract from bytes
	imports := standardImports()
	contractStruct, err := wasm.NewInstanceWithImports(contractBytes, imports)
	if err != nil {
		return nil, fmt.Errorf("couldn't instantiate contract: %v", err)
	}
	contract = &contractStruct
	contract.SetContextData(ctx{
		log:    vm.Ctx.Log,
		db:     vm.contractDB, // TODO provide each SC its own prefixed database to write to
		memory: contract.Memory,
	})

	// Set the contract's state to be what it was after last call
	state, err := vm.getContractState(db, ID)
	if err != nil {
		return nil, fmt.Errorf("couldn't get contract's state: %v", err)
	}
	memory := contract.Memory // The contract's memory

	if needMoreMemory := uint32(len(state)) > memory.Length(); needMoreMemory {
		additionalBytesNeeded := uint32(len(state)) - memory.Length()
		additionalPagesNeeded := (additionalBytesNeeded / bytesPerPage) + 1 //round up
		if err := memory.Grow(uint32(additionalPagesNeeded)); err != nil {
			return nil, fmt.Errorf("couldn't grow contract's state: %v", err)
		}
	}
	copy(memory.Data(), state)     // Copy the state over
	vm.contracts.Put(ID, contract) // put contract in cache
	return contract, nil
}

// put a contract's state (ie its whole memory) in the database
func (vm *VM) putContractState(db database.Database, ID ids.ID, state []byte) error {
	return vm.State.Put(db, stateTypeID, ID, bytes{state})
}

// get a contract's state (ie its whole memory) by its ID
func (vm *VM) getContractState(db database.Database, ID ids.ID) ([]byte, error) {
	stateIntf, err := vm.State.Get(db, stateTypeID, ID)
	if err != nil {
		return nil, err
	}
	return stateIntf.([]byte), nil
}

// Persist a transaction's status
func (vm *VM) putTxStatus(db database.Database, txID ids.ID, status choices.Status) error {
	return vm.State.PutStatus(db, txID, status)
}

// Get a transaction's status
func (vm *VM) getTxStatus(db database.Database, txID ids.ID) choices.Status {
	return vm.State.GetStatus(db, txID)
}

// Persist the return value returned by a smart invocation
// [txID] is the transaction and [value] is the returned value
func (vm *VM) putReturnValue(db database.Database, txID ids.ID, value []byte) error {
	return vm.State.Put(db, returnValueTypeID, txID, bytes{value})
}

// Get the return value returned by a smart invocation
// [txID] is the transaction
func (vm *VM) getReturnValue(db database.Database, txID ids.ID) ([]byte, error) {
	valueIntf, err := vm.State.Get(db, returnValueTypeID, txID)
	if err != nil {
		return nil, err
	}
	return valueIntf.([]byte), nil
}

func (vm *VM) registerDBTypes() error {
	unmarshalBytesFunc := func(bytes []byte) (interface{}, error) { return bytes, nil }
	if err := vm.State.RegisterType(contractBytesTypeID, unmarshalBytesFunc); err != nil {
		return fmt.Errorf("error registering contract type with state: %v", err)
	}
	if err := vm.State.RegisterType(stateTypeID, unmarshalBytesFunc); err != nil {
		return fmt.Errorf("error registering contract type with state: %v", err)
	}
	if err := vm.State.RegisterType(returnValueTypeID, unmarshalBytesFunc); err != nil {
		return fmt.Errorf("error registering return value type with state: %v", err)
	}
	return nil
}

type bytes struct {
	b []byte
}

func (bytes bytes) Bytes() []byte {
	return bytes.b
}
