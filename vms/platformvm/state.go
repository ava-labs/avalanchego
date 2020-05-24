// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
)

// This file contains methods of VM that deal with getting/putting values from database

var (
	errEmptyAccountAddress = errors.New("account has empty address")
	errNoSuchBlockchain    = errors.New("there is no blockchain with the specified ID")
)

// TODO: Cache prefixed IDs or use different way of keying into database
const (
	currentValidatorsPrefix uint64 = iota
	pendingValidatorsPrefix
	addDefaultSubnetValidatorTxIDPrefix
	addDefaultSubnetDelegatorTxIDPrefix
	addNonDefaultSubnetValidatorTxIDPrefix
	advanceTimeTxIDPrefix
	createSubnetTxIDPrefix
	createChainTxIDPrefix
	exportTxIDPrefix
	importTxIDPrefix
	rewardValidatorTxIDPrefix
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
		vm.Ctx.Log.Warn("expected to retrieve *AddStakerHeap from database but got different type")
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

// get the account with the specified Address
// If account does not exist in database, return new account
func (vm *VM) getAccount(db database.Database, address ids.ShortID) (Account, error) {
	if address.IsZero() {
		return Account{}, errEmptyAccountAddress
	}

	longID := address.LongID()

	// see if account exists
	exists, err := vm.State.Has(db, accountTypeID, longID)
	if err != nil {
		return Account{}, err
	}
	if !exists { // account doesn't exist so return new, empty account
		return Account{
			Address: address,
			Nonce:   0,
			Balance: 0,
		}, nil
	}

	accountInterface, err := vm.State.Get(db, accountTypeID, longID)
	if err != nil {
		return Account{}, err
	}
	account, ok := accountInterface.(Account)
	if !ok {
		vm.Ctx.Log.Warn("expected to retrieve Account from database but got different type")
		return Account{}, errDBAccount
	}
	return account, nil
}

// put an account in [db]
func (vm *VM) putAccount(db database.Database, account Account) error {
	err := vm.State.Put(db, accountTypeID, account.Address.LongID(), account)
	if err != nil {
		return errDBPutAccount
	}
	return nil
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
	return nil, errNoSuchBlockchain
}

// put the list of blockchains that exist to database
func (vm *VM) putChains(db database.Database, chains createChainList) error {
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
func (vm *VM) putSubnets(db database.Database, subnets CreateSubnetTxList) error {
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

func (vm *VM) getTxStatus(db database.Database, txID ids.ID) (choices.Status, error) {

	switch txType {
	case "*platformvm.addDefaultSubnetValidatorTx":
		key = txID.Prefix(addDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.addDefaultSubnetDelegatorTx":
		key = txID.Prefix(addDefaultSubnetDelegatorTxIDPrefix)
	case "*platformvm.addNonDefaultSubnetValidatorTx":
		key = txID.Prefix(addNonDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.advanceTimeTx":
		key = txID.Prefix(advanceTimeTxIDPrefix)
	case "*platformvm.CreateSubnetTx":
		key = txID.Prefix(createSubnetTxIDPrefix)
	case "*platformvm.CreateChainTx":
		key = txID.Prefix(createChainTxIDPrefix)
	case "*platformvm.ExportTx":
		key = txID.Prefix(exportTxIDPrefix)
	case "*platformvm.ImportTx":
		key = txID.Prefix(importTxIDPrefix)
	case "*platformvm.rewardValidatorTx":
		key = txID.Prefix(rewardValidatorTxIDPrefix)
	default:
		return choices.Unknown, fmt.Errorf("Unrecognized tx type %s", txType)
	}

	exists, err := vm.State.Has(db, statusTypeID, key)
	if err != nil {
		return choices.Unknown, err
	}
	if !exists { // tx doesn't exist so return choices.Unknown
		return choices.Unknown, nil
	}

	statusInterface, err := vm.State.Get(db, statusTypeID, key)
	if err != nil {
		return choices.Unknown, err
	}

	status, ok := statusInterface.(choices.Status)
	if !ok {
		vm.Ctx.Log.Warn("expected to retrieve choices.status from database but got different type")
		return choices.Unknown, errDBStatus
	}

	return status, nil

}

func (vm *VM) putTxStatus(db database.Database, txID ids.ID, status choices.Status) error {
	var key ids.ID

	switch txType {
	case "*platformvm.addDefaultSubnetValidatorTx":
		key = txID.Prefix(addDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.addDefaultSubnetDelegatorTx":
		key = txID.Prefix(addDefaultSubnetDelegatorTxIDPrefix)
	case "*platformvm.addNonDefaultSubnetValidatorTx":
		key = txID.Prefix(addNonDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.advanceTimeTx":
		key = txID.Prefix(advanceTimeTxIDPrefix)
	case "*platformvm.CreateSubnetTx":
		key = txID.Prefix(createSubnetTxIDPrefix)
	case "*platformvm.CreateChainTx":
		key = txID.Prefix(createChainTxIDPrefix)
	case "*platformvm.ExportTx":
		key = txID.Prefix(exportTxIDPrefix)
	case "*platformvm.ImportTx":
		key = txID.Prefix(importTxIDPrefix)
	case "*platformvm.rewardValidatorTx":
		key = txID.Prefix(rewardValidatorTxIDPrefix)
	default:
		return fmt.Errorf("Unrecognized tx type %s", txType)
	}

	err := vm.State.Put(db, statusTypeID, key, status)

	if err != nil {
		return errDBPutTxStatus
	}
	return nil
}

func (vm *VM) getTx(db database.Database, txID ids.ID) (*GenericTx, error) {

	var key ids.ID

	switch txType {
	case "*platformvm.addDefaultSubnetValidatorTx":
		key = txID.Prefix(addDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.addDefaultSubnetDelegatorTx":
		key = txID.Prefix(addDefaultSubnetDelegatorTxIDPrefix)
	case "*platformvm.addNonDefaultSubnetValidatorTx":
		key = txID.Prefix(addNonDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.advanceTimeTx":
		key = txID.Prefix(advanceTimeTxIDPrefix)
	case "*platformvm.CreateSubnetTx":
		key = txID.Prefix(createSubnetTxIDPrefix)
	case "*platformvm.CreateChainTx":
		key = txID.Prefix(createChainTxIDPrefix)
	case "*platformvm.ExportTx":
		key = txID.Prefix(exportTxIDPrefix)
	case "*platformvm.ImportTx":
		key = txID.Prefix(importTxIDPrefix)
	case "rewardValidatorTx":
		key = txID.Prefix(rewardValidatorTxIDPrefix)
	default:
		return nil, fmt.Errorf("Unrecognized tx type %s", txType)
	}

	exists, err := vm.State.Has(db, txTypeID, key)
	if err != nil {
		return nil, err
	}
	if !exists {
		return &GenericTx{}, nil
	}

	txInterface, err := vm.State.Get(db, txTypeID, key)
	if err != nil {
		return nil, err
	}

	tx, ok := txInterface.(*GenericTx)
	if !ok {
		vm.Ctx.Log.Warn("expected to retrieve tx from database but got different type")
		return nil, errDBTx
	}

	return tx, nil
}

func (vm *VM) putTx(db database.Database, txID ids.ID, tx *GenericTx) error {
	var key ids.ID

	switch txType {
	case "*platformvm.addDefaultSubnetValidatorTx":
		key = txID.Prefix(addDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.addDefaultSubnetDelegatorTx":
		key = txID.Prefix(addDefaultSubnetDelegatorTxIDPrefix)
	case "*platformvm.addNonDefaultSubnetValidatorTx":
		key = txID.Prefix(addNonDefaultSubnetValidatorTxIDPrefix)
	case "*platformvm.advanceTimeTx":
		key = txID.Prefix(advanceTimeTxIDPrefix)
	case "*platformvm.CreateSubnetTx":
		key = txID.Prefix(createSubnetTxIDPrefix)
	case "*platformvm.CreateChainTx":
		key = txID.Prefix(createChainTxIDPrefix)
	case "*platformvm.ExportTx":
		key = txID.Prefix(exportTxIDPrefix)
	case "*platformvm.ImportTx":
		key = txID.Prefix(importTxIDPrefix)
	case "*platformvm.rewardValidatorTx":
		key = txID.Prefix(rewardValidatorTxIDPrefix)
	default:
		return fmt.Errorf("Unrecognized tx type %s", txType)
	}

	err := vm.State.Put(db, txTypeID, key, tx)
	if err != nil {
		return errDBPutTx
	}
	return nil
}

// register each type that we'll be storing in the database
// so that [vm.State] knows how to unmarshal these types from bytes
func (vm *VM) registerDBTypes() {
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
	if err := vm.State.RegisterType(validatorsTypeID, unmarshalValidatorsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	unmarshalAccountFunc := func(bytes []byte) (interface{}, error) {
		var account Account
		if err := Codec.Unmarshal(bytes, &account); err != nil {
			return nil, err
		}
		return account, nil
	}
	if err := vm.State.RegisterType(accountTypeID, unmarshalAccountFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
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
	if err := vm.State.RegisterType(chainsTypeID, unmarshalChainsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
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
	if err := vm.State.RegisterType(subnetsTypeID, unmarshalSubnetsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	unmarshalStatusFunc := func(bytes []byte) (interface{}, error) {
		var status choices.Status
		if err := Codec.Unmarshal(bytes, &status); err != nil {
			return nil, err
		}
		return status, nil
	}
	if err := vm.State.RegisterType(statusTypeID, unmarshalStatusFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	unmarshalTxFunc := func(bytes []byte) (interface{}, error) {
		genTx := GenericTx{}
		if err := Codec.Unmarshal(bytes, &genTx); err != nil {
			return nil, err
		}
		return &genTx, nil
	}
	if err := vm.State.RegisterType(txTypeID, unmarshalTxFunc); err != nil {
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
