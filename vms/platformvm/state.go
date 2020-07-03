// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/vms/components/ava"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

// This file contains methods of VM that deal with getting/putting values from database

var (
	errEmptyAccountAddress = errors.New("account has empty address")
)

// TODO: Cache prefixed IDs or use different way of keying into database
const (
	currentValidatorsPrefix uint64 = iota
	pendingValidatorsPrefix
)

// get the validators currently validating the specified subnet
func (vm *VM) getCurrentValidators(db database.Database, subnetID ids.ID) (*EventHeap, error) {
	// if current validators aren't specified in database, return empty validator set
	key := subnetID.Prefix(currentValidatorsPrefix)
	has, err := vm.State.Has(db, validatorsTypeID, key)
	if err != nil {
		return nil, err
	}
	if !has {
		return &EventHeap{
			SortByStartTime: false,
			Txs:             make([]TimedTx, 0),
		}, nil
	}
	currentValidatorsInterface, err := vm.State.Get(db, validatorsTypeID, key)
	if err != nil {
		return nil, err
	}
	currentValidators, ok := currentValidatorsInterface.(*EventHeap)
	if !ok {
		vm.Ctx.Log.Error("expected to retrieve *AddStakerHeap from database but got different type")
		return nil, err
	}
	for _, validator := range currentValidators.Txs {
		if err := validator.initialize(vm); err != nil {
			return nil, err
		}
	}
	return currentValidators, nil
}

// put the validators currently validating the specified subnet
func (vm *VM) putCurrentValidators(db database.Database, validators *EventHeap, subnetID ids.ID) error {
	err := vm.State.Put(db, validatorsTypeID, subnetID.Prefix(currentValidatorsPrefix), validators)
	if err != nil {
		return errDBPutCurrentValidators
	}
	return nil
}

// get the validators that are slated to validate the specified subnet in the future
func (vm *VM) getPendingValidators(db database.Database, subnetID ids.ID) (*EventHeap, error) {
	// if pending validators aren't specified in database, return empty validator set
	key := subnetID.Prefix(pendingValidatorsPrefix)
	has, err := vm.State.Has(db, validatorsTypeID, key)
	if err != nil {
		return nil, err
	}
	if !has {
		return &EventHeap{
			SortByStartTime: true,
			Txs:             make([]TimedTx, 0),
		}, nil
	}
	pendingValidatorHeapInterface, err := vm.State.Get(db, validatorsTypeID, key)
	if err != nil {
		return nil, errDBPendingValidators
	}
	pendingValidatorHeap, ok := pendingValidatorHeapInterface.(*EventHeap)
	if !ok {
		vm.Ctx.Log.Error("expected to retrieve *EventHeap from database but got different type")
		return nil, errDBPendingValidators
	}
	return pendingValidatorHeap, nil
}

// put the validators that are slated to validate the specified subnet in the future
func (vm *VM) putPendingValidators(db database.Database, validators *EventHeap, subnetID ids.ID) error {
	if !validators.SortByStartTime {
		return errors.New("pending validators should be sorted by start time")
	}
	err := vm.State.Put(db, validatorsTypeID, subnetID.Prefix(pendingValidatorsPrefix), validators)
	if err != nil {
		return errDBPutPendingValidators
	}
	return nil
}

// getUTXO returns the UTXO with the specified ID
func (vm *VM) getUTXO(db database.Database, utxoID *ava.UTXOID) (*ava.UTXO, error) {
	// TODO
	return nil, errors.New("TODO")
}

// putUTXO persists the given UTXO
func (vm *VM) putUTXO(db database.Database, utxo *ava.UTXO) error {
	/*
		txID, outputIndex := utxo.InputSource()
		key := txID.Prefix(uint64(outputIndex))
		if err := vm.State.Put(db, utxoTypeID, key, utxo); err != nil {
			return err
		}
		// TODO: Map addr --> UTXOs that ref it
	*/
	return errors.New("TODO")
}

// removeUTXO removes the UTXO with the given ID
func (vm *VM) removeUTXO(db database.Database, utxoID *ava.UTXOID) error {
	// TODO
	return errors.New("TODO")
}

// return the IDs of UTXOs that reference [addr]
func (vm *VM) getRefedBy(db database.Database, addr ids.ShortID) ([]ids.ID, error) {
	// TODO
	return nil, errors.New("TODO")
}

// each element of [utxoIDs] is the ID of a UTXO that references [addr]
func (vm *VM) putRefedBy(db database.Database, addr ids.ShortID, utxoIDs []ids.ID) error {
	// TODO
	return errors.New("TODO")
}

// getUTXOs returns UTXOs that reference at least one of the addresses in [addrs]
func (vm *VM) getUTXOs(db database.Database, addrs ids.ShortSet) ([]*ava.UTXO, error) {
	/* TODO
	utxoIDs := ids.Set{}
	for _, addr := range addrs.List() {
	}
	*/
	return nil, errors.New("TODO")
}

// get all the blockchains that exist
func (vm *VM) getChains(db database.Database) ([]*CreateChainTx, error) {
	chainsInterface, err := vm.State.Get(db, chainsTypeID, chainsKey)
	if err != nil {
		return nil, err
	}
	chains, ok := chainsInterface.([]*CreateChainTx)
	if !ok {
		vm.Ctx.Log.Error("expected to retrieve []*CreateChainTx from database but got different type")
		return nil, errDBChains
	}
	return chains, nil
}

// get a blockchain by its ID
func (vm *VM) getChain(db database.Database, ID ids.ID) (*CreateChainTx, error) {
	chains, err := vm.getChains(db)
	if err != nil {
		return nil, err
	}
	for _, chain := range chains {
		if chain.ID().Equals(ID) {
			return chain, nil
		}
	}
	return nil, fmt.Errorf("blockchain %s doesn't exist", ID)
}

// put the list of blockchains that exist to database
func (vm *VM) putChains(db database.Database, chains []*CreateChainTx) error {
	if err := vm.State.Put(db, chainsTypeID, chainsKey, chains); err != nil {
		return errDBPutChains
	}
	return nil
}

// get the platfrom chain's timestamp from [db]
func (vm *VM) getTimestamp(db database.Database) (time.Time, error) {
	timestamp, err := vm.State.GetTime(db, timestampKey)
	if err != nil {
		return time.Time{}, err
	}
	return timestamp, nil
}

// put the platform chain's timestamp in [db]
func (vm *VM) putTimestamp(db database.Database, timestamp time.Time) error {
	if err := vm.State.PutTime(db, timestampKey, timestamp); err != nil {
		return err
	}
	return nil
}

// put the subnets that exist to [db]
func (vm *VM) putSubnets(db database.Database, subnets []*CreateSubnetTx) error {
	if err := vm.State.Put(db, subnetsTypeID, subnetsKey, subnets); err != nil {
		return err
	}
	return nil
}

// get the subnets that exist in [db]
func (vm *VM) getSubnets(db database.Database) ([]*CreateSubnetTx, error) {
	subnetsIntf, err := vm.State.Get(db, subnetsTypeID, subnetsKey)
	if err != nil {
		return nil, err
	}
	subnets, ok := subnetsIntf.([]*CreateSubnetTx)
	if !ok {
		vm.Ctx.Log.Warn("expected to retrieve []*CreateSubnetTx from database but got different type")
		return nil, errDB
	}
	for _, subnet := range subnets {
		subnet.vm = vm
	}
	return subnets, nil
}

// get the subnet with the specified ID
func (vm *VM) getSubnet(db database.Database, id ids.ID) (*CreateSubnetTx, error) {
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return nil, err
	}

	for _, subnet := range subnets {
		if subnet.id.Equals(id) {
			return subnet, nil
		}
	}
	return nil, fmt.Errorf("couldn't find subnet with ID %s", id)
}

// register each type that we'll be storing in the database
// so that [vm.State] knows how to unmarshal these types from bytes
func (vm *VM) registerDBTypes() {
	marshalValidatorsFunc := func(vdrsIntf interface{}) ([]byte, error) {
		if vdrs, ok := vdrsIntf.(*EventHeap); ok {
			return vdrs.Bytes()
		}
		return nil, errors.New("expected *EventHeap but got unexpected type")
	}
	unmarshalValidatorsFunc := func(bytes []byte) (interface{}, error) {
		stakers := EventHeap{}
		if err := Codec.Unmarshal(bytes, &stakers); err != nil {
			return nil, err
		}
		for _, tx := range stakers.Txs {
			if err := tx.initialize(vm); err != nil {
				return nil, err
			}
		}
		return &stakers, nil
	}
	if err := vm.State.RegisterType(validatorsTypeID, marshalValidatorsFunc, unmarshalValidatorsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalChainsFunc := func(chainsIntf interface{}) ([]byte, error) {
		if chains, ok := chainsIntf.([]*CreateChainTx); ok {
			return Codec.Marshal(chains)
		}
		return nil, errors.New("expected []*CreateChainTx but got unexpected type")
	}
	unmarshalChainsFunc := func(bytes []byte) (interface{}, error) {
		var chains []*CreateChainTx
		if err := Codec.Unmarshal(bytes, &chains); err != nil {
			return nil, err
		}
		for _, chain := range chains {
			if err := chain.initialize(vm); err != nil {
				return nil, err
			}
		}
		return chains, nil
	}
	if err := vm.State.RegisterType(chainsTypeID, marshalChainsFunc, unmarshalChainsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalSubnetsFunc := func(subnetsIntf interface{}) ([]byte, error) {
		if subnets, ok := subnetsIntf.([]*CreateSubnetTx); ok {
			return Codec.Marshal(subnets)
		}
		return nil, errors.New("expected []*CreateSubnetTx but got unexpected type")
	}
	unmarshalSubnetsFunc := func(bytes []byte) (interface{}, error) {
		var subnets []*CreateSubnetTx
		if err := Codec.Unmarshal(bytes, &subnets); err != nil {
			return nil, err
		}
		for _, subnet := range subnets {
			if err := subnet.initialize(vm); err != nil {
				return nil, err
			}
		}
		return subnets, nil
	}
	if err := vm.State.RegisterType(subnetsTypeID, marshalSubnetsFunc, unmarshalSubnetsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalUTXOFunc := func(utxoIntf interface{}) ([]byte, error) {
		if utxo, ok := utxoIntf.(*ava.UTXO); ok {
			return Codec.Marshal(utxo)
		} else if utxo, ok := utxoIntf.(ava.UTXO); ok {
			return Codec.Marshal(utxo)
		}
		return nil, errors.New("expected *ava.UTXO but got unexpected type")
	}
	unmarshalUTXOFunc := func(bytes []byte) (interface{}, error) {
		var utxo ava.UTXO
		if err := Codec.Unmarshal(bytes, &utxo); err != nil {
			return nil, err
		}
		return &utxo, nil
	}
	if err := vm.State.RegisterType(utxoTypeID, marshalUTXOFunc, unmarshalUTXOFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}
}

// Unmarshal a Block from bytes and initialize it
// The Block being unmarshaled must have had static type Block when it was marshaled
// i.e. don't do:
// block := &Abort{} (or some other type that implements block)
// bytes := codec.Marshal(block)
// instead do:
// var block Block = &Abort{} (or some other type that implements block)
// bytes := codec.Marshal(&block) (need to do &block, not block, because its an interface)
func (vm *VM) unmarshalBlockFunc(bytes []byte) (snowman.Block, error) {
	// Parse the serialized fields from bytes into a new block
	var block Block
	if err := Codec.Unmarshal(bytes, &block); err != nil {
		return nil, err
	}
	// Populate the un-serialized fields of the block
	return block, block.initialize(vm, bytes)
}
