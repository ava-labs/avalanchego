// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/prefixdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/hashing"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/components/avax"

	safemath "github.com/ava-labs/gecko/utils/math"
)

// This file contains methods of VM that deal with getting/putting values from database

// TODO: Cache prefixed IDs or use different way of keying into database
const (
	startDBPrefix  = "start"
	stopDBPrefix   = "stop"
	uptimeDBPrefix = "uptime"
)

var (
	errNoValidators = errors.New("there are no validators")
)

// persist a tx
func (vm *VM) putTx(db database.Database, ID ids.ID, tx []byte) error {
	return vm.State.Put(db, txTypeID, ID, tx)
}

// retrieve a tx
func (vm *VM) getTx(db database.Database, txID ids.ID) ([]byte, error) {
	txIntf, err := vm.State.Get(db, txTypeID, txID)
	if err != nil {
		return nil, err
	}
	if tx, ok := txIntf.([]byte); ok {
		return tx, nil
	}
	return nil, fmt.Errorf("expected tx to be []byte but is type %T", txIntf)
}

// Persist a status
func (vm *VM) putStatus(db database.Database, ID ids.ID, status Status) error {
	return vm.State.Put(db, statusTypeID, ID, status)
}

// Retrieve a status
func (vm *VM) getStatus(db database.Database, ID ids.ID) (Status, error) {
	statusIntf, err := vm.State.Get(db, statusTypeID, ID)
	if err != nil {
		return Unknown, err
	}
	if status, ok := statusIntf.(Status); ok {
		return status, nil
	}
	return Unknown, fmt.Errorf("expected status to be type Status but is type %T", statusIntf)
}

// Add a staker to subnet [subnetID]'s pending validator queue. A staker may be
// a validator or a delegator
func (vm *VM) enqueueStaker(db database.Database, subnetID ids.ID, stakerTx *Tx) error {
	var (
		staker   TimedTx
		priority byte
	)
	switch unsignedTx := stakerTx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		staker = unsignedTx
		priority = 1
	case *UnsignedAddSubnetValidatorTx:
		staker = unsignedTx
		priority = 0
	case *UnsignedAddValidatorTx:
		staker = unsignedTx
		priority = 2
	default:
		return fmt.Errorf("staker is unexpected type %T", stakerTx)
	}
	stakerID := staker.ID().Bytes() // Tx ID of this tx
	txBytes := stakerTx.Bytes()

	// Sorted by subnet ID then start time then tx ID
	prefixStart := []byte(fmt.Sprintf("%s%s", subnetID, startDBPrefix))
	prefixStartDB := prefixdb.NewNested(prefixStart, db)
	defer prefixStartDB.Close()

	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(staker.StartTime().Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(stakerID)
	if p.Err != nil {
		return fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	startKey := p.Bytes

	return prefixStartDB.Put(startKey, txBytes)
}

// Remove a staker from subnet [subnetID]'s pending validator queue. A staker
// may be a validator or a delegator
func (vm *VM) dequeueStaker(db database.Database, subnetID ids.ID, stakerTx *Tx) error {
	var (
		staker   TimedTx
		priority byte
	)
	switch unsignedTx := stakerTx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		staker = unsignedTx
		priority = 1
	case *UnsignedAddSubnetValidatorTx:
		staker = unsignedTx
		priority = 0
	case *UnsignedAddValidatorTx:
		staker = unsignedTx
		priority = 2
	default:
		return fmt.Errorf("staker is unexpected type %T", stakerTx)
	}
	stakerID := staker.ID().Bytes() // Tx ID of this tx

	// Sorted by subnet ID then start time then ID
	prefixStart := []byte(fmt.Sprintf("%s%s", subnetID, startDBPrefix))
	prefixStartDB := prefixdb.NewNested(prefixStart, db)
	defer prefixStartDB.Close()

	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(staker.StartTime().Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(stakerID)
	if p.Err != nil {
		return fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	startKey := p.Bytes

	return prefixStartDB.Delete(startKey)
}

// Add a staker to subnet [subnetID]
// A staker may be a validator or a delegator
func (vm *VM) addStaker(db database.Database, subnetID ids.ID, stakerTx *Tx) error {
	var (
		staker   TimedTx
		priority byte
	)
	switch unsignedTx := stakerTx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		staker = unsignedTx
		priority = 0
	case *UnsignedAddSubnetValidatorTx:
		staker = unsignedTx
		priority = 1
	case *UnsignedAddValidatorTx:
		staker = unsignedTx
		priority = 2
	default:
		return fmt.Errorf("staker is unexpected type %T", stakerTx)
	}
	stakerID := staker.ID().Bytes() // Tx ID of this tx
	txBytes := stakerTx.Bytes()

	// Sorted by subnet ID then stop time then tx ID
	prefixStop := []byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix))
	prefixStopDB := prefixdb.NewNested(prefixStop, db)
	defer prefixStopDB.Close()

	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(staker.EndTime().Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(stakerID)
	if p.Err != nil {
		return fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	stopKey := p.Bytes

	return prefixStopDB.Put(stopKey, txBytes)
}

// Remove a staker from subnet [subnetID]
// A staker may be a validator or a delegator
func (vm *VM) removeStaker(db database.Database, subnetID ids.ID, stakerTx *Tx) error {
	var (
		staker   TimedTx
		priority byte
	)
	switch unsignedTx := stakerTx.UnsignedTx.(type) {
	case *UnsignedAddDelegatorTx:
		staker = unsignedTx
		priority = 0
	case *UnsignedAddSubnetValidatorTx:
		staker = unsignedTx
		priority = 1
	case *UnsignedAddValidatorTx:
		staker = unsignedTx
		priority = 2
	default:
		return fmt.Errorf("staker is unexpected type %T", stakerTx)
	}
	stakerID := staker.ID().Bytes() // Tx ID of this tx

	// Sorted by subnet ID then stop time
	prefixStop := []byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix))
	prefixStopDB := prefixdb.NewNested(prefixStop, db)
	defer prefixStopDB.Close()

	p := wrappers.Packer{MaxSize: wrappers.LongLen + wrappers.ByteLen + hashing.HashLen}
	p.PackLong(uint64(staker.EndTime().Unix()))
	p.PackByte(priority)
	p.PackFixedBytes(stakerID)
	if p.Err != nil {
		return fmt.Errorf("couldn't serialize validator key: %w", p.Err)
	}
	stopKey := p.Bytes

	return prefixStopDB.Delete(stopKey)
}

// Returns the pending staker that will start staking next
func (vm *VM) nextStakerStart(db database.Database, subnetID ids.ID) (*Tx, error) {
	iter := prefixdb.NewNested([]byte(fmt.Sprintf("%s%s", subnetID, startDBPrefix)), db).NewIterator()
	defer iter.Release()

	if !iter.Next() {
		return nil, errNoValidators
	}
	// Key: [Staker start time] | [Tx ID]
	// Value: Byte repr. of tx that added this validator

	tx := Tx{}
	if err := Codec.Unmarshal(iter.Value(), &tx); err != nil {
		return nil, err
	}
	return &tx, tx.Sign(vm.codec, nil)
}

// Returns the current staker that will stop staking next
func (vm *VM) nextStakerStop(db database.Database, subnetID ids.ID) (*Tx, error) {
	iter := prefixdb.NewNested([]byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix)), db).NewIterator()
	defer iter.Release()

	if !iter.Next() {
		return nil, errNoValidators
	}
	// Key: [Staker stop time] | [Tx ID]
	// Value: Byte repr. of tx that added this validator

	tx := Tx{}
	if err := Codec.Unmarshal(iter.Value(), &tx); err != nil {
		return nil, err
	}
	return &tx, tx.Sign(vm.codec, nil)
}

// Returns true if [nodeID] is a validator (not a delegator) of subnet [subnetID]
func (vm *VM) isValidator(db database.Database, subnetID ids.ID, nodeID ids.ShortID) (TimedTx, bool, error) {
	iter := prefixdb.NewNested([]byte(fmt.Sprintf("%s%s", subnetID, stopDBPrefix)), db).NewIterator()
	defer iter.Release()

	for iter.Next() {
		txBytes := iter.Value()
		tx := Tx{}
		if err := Codec.Unmarshal(txBytes, &tx); err != nil {
			return nil, false, err
		}
		if err := tx.Sign(vm.codec, nil); err != nil {
			return nil, false, err
		}

		switch vdr := tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			if subnetID.Equals(constants.PrimaryNetworkID) && vdr.Validator.NodeID.Equals(nodeID) {
				return vdr, true, nil
			}
		case *UnsignedAddSubnetValidatorTx:
			if subnetID.Equals(vdr.Validator.SubnetID()) && vdr.Validator.NodeID.Equals(nodeID) {
				return vdr, true, nil
			}
		}
	}
	return nil, false, nil
}

// Returns true if [nodeID] will be a validator (not a delegator) of subnet
// [subnetID]
func (vm *VM) willBeValidator(db database.Database, subnetID ids.ID, nodeID ids.ShortID) (TimedTx, bool, error) {
	iter := prefixdb.NewNested([]byte(fmt.Sprintf("%s%s", subnetID, startDBPrefix)), db).NewIterator()
	defer iter.Release()

	for iter.Next() {
		txBytes := iter.Value()
		tx := Tx{}
		if err := Codec.Unmarshal(txBytes, &tx); err != nil {
			return nil, false, err
		}
		if err := tx.Sign(vm.codec, nil); err != nil {
			return nil, false, err
		}

		switch vdr := tx.UnsignedTx.(type) {
		case *UnsignedAddValidatorTx:
			if subnetID.Equals(constants.PrimaryNetworkID) && vdr.Validator.NodeID.Equals(nodeID) {
				return vdr, true, nil
			}
		case *UnsignedAddSubnetValidatorTx:
			if subnetID.Equals(vdr.Validator.SubnetID()) && vdr.Validator.NodeID.Equals(nodeID) {
				return vdr, true, nil
			}
		}
	}
	return nil, false, nil
}

// getUTXO returns the UTXO with the specified ID
func (vm *VM) getUTXO(db database.Database, ID ids.ID) (*avax.UTXO, error) {
	utxoIntf, err := vm.State.Get(db, utxoTypeID, ID)
	if err != nil {
		return nil, err
	}
	utxo, ok := utxoIntf.(*avax.UTXO)
	if !ok {
		err := fmt.Errorf("expected UTXO from database but got %T", utxoIntf)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	return utxo, nil
}

// putUTXO persists the given UTXO
func (vm *VM) putUTXO(db database.Database, utxo *avax.UTXO) error {
	utxoID := utxo.InputID()
	if err := vm.State.Put(db, utxoTypeID, utxoID, utxo); err != nil {
		return err
	}

	// If this output lists addresses that it references index it
	if addressable, ok := utxo.Out.(avax.Addressable); ok {
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
	if addressable, ok := utxo.Out.(avax.Addressable); ok {
		// For each owner of this UTXO, remove from their list of UTXOs
		for _, addrBytes := range addressable.Addresses() {
			if err := vm.removeReferencingUTXO(db, addrBytes, utxoID); err != nil {
				return fmt.Errorf("couldn't update UTXO set of address %s", formatting.CB58{Bytes: addrBytes})
			}
		}
	}
	return nil
}

// Return the IDs of UTXOs that reference [addr].
// Only returns UTXOs after [start].
// Returns at most [limit] UTXO IDs.
// Returns nil if no UTXOs reference [addr].
func (vm *VM) getReferencingUTXOs(db database.Database, addr []byte, start ids.ID, limit int) (ids.Set, error) {
	toFetch := limit
	utxoIDs := ids.Set{}
	iter := prefixdb.NewNested(addr, db).NewIteratorWithStart(start.Bytes())
	defer iter.Release()
	for toFetch > 0 && iter.Next() {
		if utxoID, err := ids.ToID(iter.Key()); err != nil {
			return nil, err
		} else if !utxoID.Equals(start) {
			utxoIDs.Add(utxoID)
			toFetch--
		}
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

// GetUTXOs returns UTXOs such that at least one of the addresses in [addrs] is referenced.
// Assumed elements of [addrs] are unique.
// Returns at most [limit] UTXOs.
// If [limit] <= 0 or [limit] > maxUTXOsToFetch, it is set to [maxUTXOsToFetch].
// Only returns UTXOs associated with addresses >= [startAddr].
// For address [startAddr], only returns UTXOs whose IDs are greater than [startUTXOID].
// Returns:
// * The fetched of UTXOs
// * The address associated with the last UTXO fetched
// * The ID of the last UTXO fetched
func (vm *VM) GetUTXOs(
	db database.Database,
	addrs ids.ShortSet,
	startAddr ids.ShortID,
	startUTXOID ids.ID,
	limit int,
) ([]*avax.UTXO, ids.ShortID, ids.ID, error) {
	if limit <= 0 || limit > maxUTXOsToFetch { // Don't fetch more than [maxUTXOsToFetch]
		limit = maxUTXOsToFetch
	}

	seen := ids.Set{} // IDs of UTXOs already in the list
	utxos := make([]*avax.UTXO, 0, limit)
	lastAddr := ids.ShortEmpty
	lastIndex := ids.Empty
	addrsList := addrs.List()
	ids.SortShortIDs(addrsList)
	for _, addr := range addrs.List() {
		start := ids.Empty
		if comp := bytes.Compare(addr.Bytes(), startAddr.Bytes()); comp == -1 { // Skip addresses before [startAddr]
			continue
		} else if comp == 0 {
			start = startUTXOID
		}
		utxoIDs, err := vm.getReferencingUTXOs(db, addr.Bytes(), start, limit) // Get IDs of UTXOs to fetch
		if err != nil {
			return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXOs for address %s", addr)
		}
		for _, utxoID := range utxoIDs.List() { // Get the UTXOs
			if seen.Contains(utxoID) { // already have this UTXO in the list
				continue
			}
			utxo, err := vm.getUTXO(db, utxoID)
			if err != nil {
				return nil, ids.ShortID{}, ids.ID{}, fmt.Errorf("couldn't get UTXO %s: %w", utxoID, err)
			}
			utxos = append(utxos, utxo)
			seen.Add(utxoID)
			lastAddr = addr
			lastIndex = utxoID
			limit--
			if limit <= 0 {
				break // Found [limit] utxos; stop.
			}
		}
	}
	return utxos, lastAddr, lastIndex, nil
}

// getBalance returns the balance of [addrs]
func (vm *VM) getBalance(db database.Database, addrs ids.ShortSet) (uint64, error) {
	utxos, _, _, err := vm.GetUTXOs(db, addrs, ids.ShortEmpty, ids.Empty, -1)
	if err != nil {
		return 0, fmt.Errorf("couldn't get UTXOs: %w", err)
	}
	balance := uint64(0)
	for _, utxo := range utxos {
		if out, ok := utxo.Out.(avax.Amounter); ok {
			if balance, err = safemath.Add64(out.Amount(), balance); err != nil {
				return 0, err
			}
		}
	}
	return balance, nil
}

// get all the blockchains that exist
func (vm *VM) getChains(db database.Database) ([]*Tx, error) {
	chainsInterface, err := vm.State.Get(db, chainsTypeID, chainsKey)
	if err != nil {
		return nil, err
	}
	chains, ok := chainsInterface.([]*Tx)
	if !ok {
		err := fmt.Errorf("expected to retrieve []*CreateChainTx from database but got type %T", chainsInterface)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	return chains, nil
}

// get a blockchain by its ID
func (vm *VM) getChain(db database.Database, ID ids.ID) (*Tx, error) {
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
func (vm *VM) putChains(db database.Database, chains []*Tx) error {
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
func (vm *VM) putSubnets(db database.Database, subnets []*Tx) error {
	return vm.State.Put(db, subnetsTypeID, subnetsKey, subnets)
}

// get the subnets that exist in [db]
func (vm *VM) getSubnets(db database.Database) ([]*Tx, error) {
	subnetsIntf, err := vm.State.Get(db, subnetsTypeID, subnetsKey)
	if err != nil {
		return nil, err
	}
	subnets, ok := subnetsIntf.([]*Tx)
	if !ok {
		err := fmt.Errorf("expected to retrieve []*CreateSubnetTx from database but got type %T", subnetsIntf)
		vm.Ctx.Log.Error(err.Error())
		return nil, err
	}
	for _, tx := range subnets {
		if err := tx.Sign(vm.codec, nil); err != nil {
			return nil, err
		}
	}
	return subnets, nil
}

// get the subnet with the specified ID
func (vm *VM) getSubnet(db database.Database, id ids.ID) (*Tx, TxError) {
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
			if err := tx.Sign(vm.codec, nil); err != nil {
				return nil, err
			}
		}
		return &stakers, nil
	}
	if err := vm.State.RegisterType(validatorsTypeID, marshalValidatorsFunc, unmarshalValidatorsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalChainsFunc := func(chainsIntf interface{}) ([]byte, error) {
		if chains, ok := chainsIntf.([]*Tx); ok {
			return Codec.Marshal(chains)
		}
		return nil, fmt.Errorf("expected []*CreateChainTx but got type %T", chainsIntf)
	}
	unmarshalChainsFunc := func(bytes []byte) (interface{}, error) {
		var chains []*Tx
		if err := Codec.Unmarshal(bytes, &chains); err != nil {
			return nil, err
		}
		for _, tx := range chains {
			if err := tx.Sign(vm.codec, nil); err != nil {
				return nil, err
			}
		}
		return chains, nil
	}
	if err := vm.State.RegisterType(chainsTypeID, marshalChainsFunc, unmarshalChainsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalSubnetsFunc := func(subnetsIntf interface{}) ([]byte, error) {
		if subnets, ok := subnetsIntf.([]*Tx); ok {
			return Codec.Marshal(subnets)
		}
		return nil, fmt.Errorf("expected []*Tx but got type %T", subnetsIntf)
	}
	unmarshalSubnetsFunc := func(bytes []byte) (interface{}, error) {
		var subnets []*Tx
		if err := Codec.Unmarshal(bytes, &subnets); err != nil {
			return nil, err
		}
		for _, tx := range subnets {
			if err := tx.Sign(vm.codec, nil); err != nil {
				return nil, err
			}
		}
		return subnets, nil
	}
	if err := vm.State.RegisterType(subnetsTypeID, marshalSubnetsFunc, unmarshalSubnetsFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalUTXOFunc := func(utxoIntf interface{}) ([]byte, error) {
		if utxo, ok := utxoIntf.(*avax.UTXO); ok {
			return Codec.Marshal(utxo)
		} else if utxo, ok := utxoIntf.(avax.UTXO); ok {
			return Codec.Marshal(utxo)
		}
		return nil, fmt.Errorf("expected *avax.UTXO but got type %T", utxoIntf)
	}
	unmarshalUTXOFunc := func(bytes []byte) (interface{}, error) {
		var utxo avax.UTXO
		if err := Codec.Unmarshal(bytes, &utxo); err != nil {
			return nil, err
		}
		return &utxo, nil
	}
	if err := vm.State.RegisterType(utxoTypeID, marshalUTXOFunc, unmarshalUTXOFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalTxFunc := func(txIntf interface{}) ([]byte, error) {
		if tx, ok := txIntf.([]byte); ok {
			return tx, nil
		}
		return nil, fmt.Errorf("expected []byte but got type %T", txIntf)
	}
	unmarshalTxFunc := func(bytes []byte) (interface{}, error) {
		return bytes, nil
	}
	if err := vm.State.RegisterType(txTypeID, marshalTxFunc, unmarshalTxFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}

	marshalStatusFunc := func(statusIntf interface{}) ([]byte, error) {
		if status, ok := statusIntf.(Status); ok {
			return vm.codec.Marshal(status)
		}
		return nil, fmt.Errorf("expected Status but got type %T", statusIntf)
	}
	unmarshalStatusFunc := func(bytes []byte) (interface{}, error) {
		var status Status
		if err := Codec.Unmarshal(bytes, &status); err != nil {
			return nil, err
		}
		return status, nil
	}
	if err := vm.State.RegisterType(statusTypeID, marshalStatusFunc, unmarshalStatusFunc); err != nil {
		vm.Ctx.Log.Warn(errRegisteringType.Error())
	}
}

type validatorUptime struct {
	UpDuration  uint64 `serialize:"true"` // In seconds
	LastUpdated uint64 `serialize:"true"` // Unix time in seconds
}

func (vm *VM) uptime(db database.Database, nodeID ids.ShortID) (*validatorUptime, error) {
	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), db)
	defer uptimeDB.Close()

	uptimeBytes, err := uptimeDB.Get(nodeID.Bytes())
	if err != nil {
		return nil, err
	}

	uptime := validatorUptime{}
	if err := Codec.Unmarshal(uptimeBytes, &uptime); err != nil {
		return nil, err
	}
	return &uptime, nil
}
func (vm *VM) setUptime(db database.Database, nodeID ids.ShortID, uptime *validatorUptime) error {
	uptimeBytes, err := Codec.Marshal(uptime)
	if err != nil {
		return err
	}

	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), db)
	defer uptimeDB.Close()

	return uptimeDB.Put(nodeID.Bytes(), uptimeBytes)
}
func (vm *VM) deleteUptime(db database.Database, nodeID ids.ShortID) error {
	uptimeDB := prefixdb.NewNested([]byte(uptimeDBPrefix), db)
	defer uptimeDB.Close()

	return uptimeDB.Delete(nodeID.Bytes())
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
