// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/vms/components/ava"

	safemath "github.com/ava-labs/gecko/utils/math"
)

// This file contains methods of VM that deal with getting/putting values from database

// TODO: Cache prefixed IDs or use different way of keying into database
const (
	currentValidatorsPrefix uint64 = iota
	pendingValidatorsPrefix
)

// perist a tx
func (vm *VM) putTx(db database.Database, tx *WrappedTx) error {
	return vm.State.Put(db, txTypeID, tx.ID, tx)
}

func (vm *VM) getTx(db database.Database, txID ids.ID) (*WrappedTx, error) {
	txIntf, err := vm.State.Get(db, txTypeID, txID)
	if err != nil {
		return nil, err
	}
	if tx, ok := txIntf.(*WrappedTx); ok {
		return tx, nil
	}
	return nil, fmt.Errorf("expected tx to be []byte but is type %T", txIntf)
}

// get the validators currently validating the specified subnet
func (vm *VM) getCurrentValidators(db database.Database, subnetID ids.ID) (*EventHeap, error) {
	return vm.getValidatorsFromDB(db, subnetID, currentValidatorsPrefix, false)
}

// put the validators currently validating the specified subnet
func (vm *VM) putCurrentValidators(db database.Database, validators *EventHeap, subnetID ids.ID) error {
	if validators.SortByStartTime {
		return errors.New("current validators should be sorted by end time")
	}
	err := vm.State.Put(db, validatorsTypeID, subnetID.Prefix(currentValidatorsPrefix), validators)
	if err != nil {
		return fmt.Errorf("couldn't put current validator set: %w", err)
	}
	return nil
}

// get the validators that are slated to validate the specified subnet in the future
func (vm *VM) getPendingValidators(db database.Database, subnetID ids.ID) (*EventHeap, error) {
	return vm.getValidatorsFromDB(db, subnetID, pendingValidatorsPrefix, true)
}

// put the validators that are slated to validate the specified subnet in the future
func (vm *VM) putPendingValidators(db database.Database, validators *EventHeap, subnetID ids.ID) error {
	if !validators.SortByStartTime {
		return errors.New("pending validators should be sorted by start time")
	}
	err := vm.State.Put(db, validatorsTypeID, subnetID.Prefix(pendingValidatorsPrefix), validators)
	if err != nil {
		return fmt.Errorf("couldn't put pending validator set: %w", err)
	}
	return nil
}

// get the validators currently validating the specified subnet
func (vm *VM) getValidatorsFromDB(
	db database.Database,
	subnetID ids.ID,
	prefix uint64,
	sortByStartTime bool,
) (*EventHeap, error) {
	// if current validators aren't specified in database, return empty validator set
	key := subnetID.Prefix(prefix)
	has, err := vm.State.Has(db, validatorsTypeID, key)
	if err != nil {
		return nil, err
	}
	if !has {
		return &EventHeap{SortByStartTime: sortByStartTime}, nil
	}
	validatorsInterface, err := vm.State.Get(db, validatorsTypeID, key)
	if err != nil {
		return nil, err
	}
	validators, ok := validatorsInterface.(*EventHeap)
	if !ok {
		err := fmt.Errorf("expected to retrieve *EventHeap from database but got type %T", validatorsInterface)
		vm.Ctx.Log.Error("error while fetching validators: %s", err)
		return nil, err
	}
	for _, validator := range validators.Txs {
		txBytes, err := vm.codec.Marshal(validator)
		if err != nil {
			return nil, err
		}
		if err := validator.initialize(vm, txBytes); err != nil {
			return nil, err
		}
	}
	return validators, nil
}

// getUTXO returns the UTXO with the specified ID
func (vm *VM) getUTXO(db database.Database, ID ids.ID) (*ava.UTXO, error) {
	utxoIntf, err := vm.State.Get(db, utxoTypeID, ID)
	if err != nil {
		return nil, err
	}
	utxo, ok := utxoIntf.(*ava.UTXO)
	if !ok {
		err := fmt.Errorf("expected UTXO from database but got %T", utxoIntf)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	return utxo, nil
}

// putUTXO persists the given UTXO
func (vm *VM) putUTXO(db database.Database, utxo *ava.UTXO) error {
	utxoID := utxo.InputID()
	if err := vm.State.Put(db, utxoTypeID, utxoID, utxo); err != nil {
		return err
	}

	// If this output lists addresses that it references index it
	if addressable, ok := utxo.Out.(ava.Addressable); ok {
		// For each owner of this UTXO, add to list of UTXOs owned by that addr
		for _, addrBytes := range addressable.Addresses() {
			if err := vm.putReferencingUTXO(db, addrBytes, utxoID); err != nil {
				return fmt.Errorf("couldn't update UTXO set of address %s", formatting.CB58{Bytes: addrBytes})
			}
		}
	}
	return nil
}

// removeUTXO removes the UTXO with the given ID
// If the utxo doesn't exist, returns nil
func (vm *VM) removeUTXO(db database.Database, utxoID ids.ID) error {
	utxo, err := vm.getUTXO(db, utxoID) // Get the UTXO
	if err != nil {
		return nil
	}
	if err := vm.State.Put(db, utxoTypeID, utxoID, nil); err != nil { // remove the UTXO
		return err
	}
	// If this output lists addresses that it references remove the indices
	if addressable, ok := utxo.Out.(ava.Addressable); ok {
		// For each owner of this UTXO, remove from their list of UTXOs
		for _, addrBytes := range addressable.Addresses() {
			if err := vm.removeReferencingUTXO(db, addrBytes, utxoID); err != nil {
				return fmt.Errorf("couldn't update UTXO set of address %s", formatting.CB58{Bytes: addrBytes})
			}
		}
	}
	return nil
}

// return the IDs of UTXOs that reference [addr]
// Returns nil if no UTXOs reference [addr]
func (vm *VM) getReferencingUTXOs(db database.Database, addrBytes []byte) (ids.Set, error) {
	utxoIDs := ids.Set{}
	iter := prefixdb.NewNested(addrBytes, db).NewIterator()
	defer iter.Release()
	for iter.Next() {
		utxoID, err := ids.ToID(iter.Key())
		if err != nil {
			return nil, err
		}
		utxoIDs.Add(utxoID)
	}
	return utxoIDs, nil
}

// Persist that the UTXO with ID [utxoID] references [addr]
func (vm *VM) putReferencingUTXO(db database.Database, addrBytes []byte, utxoID ids.ID) error {
	prefixedDB := prefixdb.NewNested(addrBytes, db)
	return prefixedDB.Put(utxoID.Bytes(), nil)
}

// Remove the UTXO with ID [utxoID] from the set of UTXOs that reference [addr]
func (vm *VM) removeReferencingUTXO(db database.Database, addrBytes []byte, utxoID ids.ID) error {
	prefixedDB := prefixdb.NewNested(addrBytes, db)
	return prefixedDB.Delete(utxoID.Bytes())
}

// getUTXOs returns UTXOs that reference at least one of the addresses in [addrs]
func (vm *VM) getUTXOs(db database.Database, addrs [][]byte) ([]*ava.UTXO, error) {
	utxoIDs := ids.Set{}
	for _, addr := range addrs {
		addrUTXOs, err := vm.getReferencingUTXOs(db, addr)
		if err != nil {
			return nil, fmt.Errorf("couldn't get UTXOs for address %s", addr)
		}
		utxoIDs.Union(addrUTXOs)
	}
	utxos := make([]*ava.UTXO, utxoIDs.Len())
	for i, utxoID := range utxoIDs.List() {
		utxo, err := vm.getUTXO(db, utxoID)
		if err != nil {
			return nil, fmt.Errorf("couldn't get UTXO %s: %w", utxoID, err)
		}
		utxos[i] = utxo
	}
	return utxos, nil
}

// getBalance returns the balance of [addrs]
func (vm *VM) getBalance(db database.Database, addrs [][]byte) (uint64, error) {
	utxos, err := vm.getUTXOs(db, addrs)
	if err != nil {
		return 0, fmt.Errorf("couldn't get UTXOs: %w", err)
	}
	balance := uint64(0)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(ava.Amounter); ok {
			if balance, err = safemath.Add64(out.Amount(), balance); err != nil {
				return 0, err
			}
		}
	}
	return balance, nil
}

// get all the blockchains that exist
func (vm *VM) getChains(db database.Database) ([]*DecisionTx, error) {
	chainsInterface, err := vm.State.Get(db, chainsTypeID, chainsKey)
	if err != nil {
		return nil, err
	}
	chains, ok := chainsInterface.([]*DecisionTx)
	if !ok {
		err := fmt.Errorf("expected to retrieve []*CreateChainTx from database but got type %T", chainsInterface)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	return chains, nil
}

// get a blockchain by its ID
func (vm *VM) getChain(db database.Database, ID ids.ID) (*DecisionTx, error) {
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
func (vm *VM) putChains(db database.Database, chains []*DecisionTx) error {
	return vm.State.Put(db, chainsTypeID, chainsKey, chains)
}

// get the platfrom chain's timestamp from [db]
func (vm *VM) getTimestamp(db database.Database) (time.Time, error) {
	return vm.State.GetTime(db, timestampKey)
}

// put the platform chain's timestamp in [db]
func (vm *VM) putTimestamp(db database.Database, timestamp time.Time) error {
	return vm.State.PutTime(db, timestampKey, timestamp)
}

// put the subnets that exist to [db]
func (vm *VM) putSubnets(db database.Database, subnets []*DecisionTx) error {
	return vm.State.Put(db, subnetsTypeID, subnetsKey, subnets)
}

// get the subnets that exist in [db]
func (vm *VM) getSubnets(db database.Database) ([]*DecisionTx, error) {
	subnetsIntf, err := vm.State.Get(db, subnetsTypeID, subnetsKey)
	if err != nil {
		return nil, err
	}
	subnets, ok := subnetsIntf.([]*DecisionTx)
	if !ok {
		err := fmt.Errorf("expected to retrieve []*CreateSubnetTx from database but got type %T", subnetsIntf)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	for _, subnet := range subnets {
		txBytes, err := vm.codec.Marshal(subnet)
		if err != nil {
			return nil, err
		}
		subnet.initialize(vm, txBytes)
	}
	return subnets, nil
}

// get the subnet with the specified ID
func (vm *VM) getSubnet(db database.Database, id ids.ID) (*DecisionTx, TxError) {
	subnets, err := vm.getSubnets(db)
	if err != nil {
		return nil, tempError{err}
	}

	for _, subnet := range subnets {
		if subnet.ID().Equals(id) {
			return subnet, nil
		}
	}
	return nil, permError{fmt.Errorf("couldn't find subnet with ID %s", id)}
}

// Returns the height of the preferred block
func (vm *VM) preferredHeight() (uint64, error) {
	preferred, err := vm.getBlock(vm.Preferred())
	if err != nil {
		return 0, err
	}
	return preferred.Height(), nil
}

// register each type that we'll be storing in the database
// so that [vm.State] knows how to unmarshal these types from bytes
func (vm *VM) registerDBTypes() {
	marshalValidatorsFunc := func(vdrsIntf interface{}) ([]byte, error) {
		if vdrs, ok := vdrsIntf.(*EventHeap); ok {
			return vdrs.Bytes()
		}
		return nil, fmt.Errorf("expected *EventHeap but got type %T", vdrsIntf)
	}
	unmarshalValidatorsFunc := func(bytes []byte) (interface{}, error) {
		stakers := EventHeap{}
		if err := Codec.Unmarshal(bytes, &stakers); err != nil {
			return nil, err
		}
		for _, tx := range stakers.Txs {
			txBytes, err := vm.codec.Marshal(tx)
			if err != nil {
				return nil, err
			}
			if err := tx.initialize(vm, txBytes); err != nil {
				return nil, err
			}
		}
		return &stakers, nil
	}
	if err := vm.State.RegisterType(validatorsTypeID, marshalValidatorsFunc, unmarshalValidatorsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalChainsFunc := func(chainsIntf interface{}) ([]byte, error) {
		if chains, ok := chainsIntf.([]*DecisionTx); ok {
			return Codec.Marshal(chains)
		}
		return nil, fmt.Errorf("expected []*CreateChainTx but got type %T", chainsIntf)
	}
	unmarshalChainsFunc := func(bytes []byte) (interface{}, error) {
		var chains []*DecisionTx
		if err := Codec.Unmarshal(bytes, &chains); err != nil {
			return nil, err
		}
		for _, chain := range chains {
			txBytes, err := vm.codec.Marshal(chain)
			if err != nil {
				return nil, err
			}
			if err := chain.initialize(vm, txBytes); err != nil {
				return nil, err
			}
		}
		return chains, nil
	}
	if err := vm.State.RegisterType(chainsTypeID, marshalChainsFunc, unmarshalChainsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalSubnetsFunc := func(subnetsIntf interface{}) ([]byte, error) {
		if subnets, ok := subnetsIntf.([]*DecisionTx); ok {
			return Codec.Marshal(subnets)
		}
		return nil, fmt.Errorf("expected []*DecisionTx but got type %T", subnetsIntf)
	}
	unmarshalSubnetsFunc := func(bytes []byte) (interface{}, error) {
		var subnets []*DecisionTx
		if err := Codec.Unmarshal(bytes, &subnets); err != nil {
			return nil, err
		}
		for _, subnet := range subnets {
			txBytes, err := vm.codec.Marshal(subnet)
			if err != nil {
				return nil, err
			}
			if err := subnet.initialize(vm, txBytes); err != nil {
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
		return nil, fmt.Errorf("expected *ava.UTXO but got type %T", utxoIntf)
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

	marshalTxFunc := func(txIntf interface{}) ([]byte, error) {
		if tx, ok := txIntf.(*WrappedTx); ok {
			return Codec.Marshal(tx)
		}
		return nil, fmt.Errorf("expected *WrappedTx but got type %T", txIntf)
	}
	unmarshalTxFunc := func(bytes []byte) (interface{}, error) {
		var tx WrappedTx
		if err := Codec.Unmarshal(bytes, &tx); err != nil {
			return nil, err
		}
		return &tx, nil
	}
	if err := vm.State.RegisterType(txTypeID, marshalTxFunc, unmarshalTxFunc); err != nil {
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
