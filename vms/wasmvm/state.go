package wasmvm

import (
	"fmt"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
)

const (
	contractTypeID uint64 = iota
	stateTypeID
)

// put a contract (in its raw byte form) in the database
func (vm *VM) putContract(db database.Database, ID ids.ID, contract []byte) error {
	return vm.State.Put(db, contractTypeID, ID, bytes{contract})
}

// get a contract (in its raw byte form) by its ID
func (vm *VM) getContract(db database.Database, ID ids.ID) ([]byte, error) {
	contractIntf, err := vm.State.Get(db, contractTypeID, ID)
	if err != nil {
		return nil, err
	}
	return contractIntf.([]byte), nil
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

func (vm *VM) registerDBTypes() error {
	unmarshalBytesFunc := func(bytes []byte) (interface{}, error) { return bytes, nil }
	if err := vm.State.RegisterType(contractTypeID, unmarshalBytesFunc); err != nil {
		return fmt.Errorf("error registering contract type with state: %v", err)
	}
	if err := vm.State.RegisterType(stateTypeID, unmarshalBytesFunc); err != nil {
		return fmt.Errorf("error registering contract type with state: %v", err)
	}
	return nil
}

type bytes struct {
	b []byte
}

func (bytes bytes) Bytes() []byte {
	return bytes.b
}
