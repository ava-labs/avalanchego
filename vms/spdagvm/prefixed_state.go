// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spdagvm

import (
	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	txID uint64 = iota
	utxoID
	txStatusID
	fundsID
	dbInitializedID
)

var (
	dbInitialized = ids.Empty.Prefix(dbInitializedID)
)

// prefixedState wraps a state object. By prefixing the state, there will be no
// collisions between different types of objects that have the same hash.
type prefixedState struct {
	state *state

	tx, utxo, txStatus, funds cache.Cacher
	uniqueTx                  cache.Deduplicator
}

// UniqueTx de-duplicates the transaction.
func (s *prefixedState) UniqueTx(tx *UniqueTx) *UniqueTx {
	return s.uniqueTx.Deduplicate(tx).(*UniqueTx)
}

// Tx loads the transaction whose ID is [id] from storage.
func (s *prefixedState) Tx(id ids.ID) (*Tx, error) { return s.state.Tx(s.uniqueID(id, txID, s.tx)) }

// SetTx saves transaction [tx], whose ID is [id], to storage.
func (s *prefixedState) SetTx(id ids.ID, tx *Tx) error {
	return s.state.SetTx(s.uniqueID(id, txID, s.tx), tx)
}

// UTXO loads a UTXO from storage.
func (s *prefixedState) UTXO(id ids.ID) (*UTXO, error) {
	return s.state.UTXO(s.uniqueID(id, utxoID, s.utxo))
}

// SetUTXO saves the provided utxo to storage.
func (s *prefixedState) SetUTXO(id ids.ID, utxo *UTXO) error {
	return s.state.SetUTXO(s.uniqueID(id, utxoID, s.utxo), utxo)
}

// Status returns the status of the transaction whose ID is [id] from storage.
func (s *prefixedState) Status(id ids.ID) (choices.Status, error) {
	return s.state.Status(s.uniqueID(id, txStatusID, s.txStatus))
}

// SetStatus saves the status of [id] as [status]
func (s *prefixedState) SetStatus(id ids.ID, status choices.Status) error {
	return s.state.SetStatus(s.uniqueID(id, txStatusID, s.txStatus), status)
}

// DBInitialized returns the status of this database. If the database is
// uninitialized, the status will be unknown.
func (s *prefixedState) DBInitialized() (choices.Status, error) { return s.state.Status(dbInitialized) }

// SetDBInitialized saves the provided status of the database.
func (s *prefixedState) SetDBInitialized(status choices.Status) error {
	return s.state.SetStatus(dbInitialized, status)
}

// Funds returns the IDs of unspent UTXOs that reference address [addr]
func (s *prefixedState) Funds(addr ids.ID) ([]ids.ID, error) {
	return s.state.IDs(s.uniqueID(addr, fundsID, s.funds))
}

// SetFunds saves the mapping from address [addr] to the IDs of unspent UTXOs
// that reference [addr]
func (s *prefixedState) SetFunds(addr ids.ID, idSlice []ids.ID) error {
	return s.state.SetIDs(s.uniqueID(addr, fundsID, s.funds), idSlice)
}

// Make [id] unique by prefixing [prefix] to it
func (s *prefixedState) uniqueID(id ids.ID, prefix uint64, cacher cache.Cacher) ids.ID {
	if cachedIDIntf, found := cacher.Get(id); found {
		return cachedIDIntf.(ids.ID)
	}
	uID := id.Prefix(prefix)
	cacher.Put(id, uID)
	return uID
}

// SpendUTXO consumes the utxo whose ID is [utxoID]
func (s *prefixedState) SpendUTXO(utxoID ids.ID) error {
	utxo, err := s.UTXO(utxoID)
	if err != nil {
		return err
	}
	if err := s.SetUTXO(utxoID, nil); err != nil {
		return err
	}

	// Update funds
	switch out := utxo.Out().(type) {
	case *OutputPayment:
		return s.removeUTXO(out.Addresses(), utxoID)
	case *OutputTakeOrLeave:
		errs := wrappers.Errs{}
		errs.Add(s.removeUTXO(out.Addresses1(), utxoID))
		errs.Add(s.removeUTXO(out.Addresses2(), utxoID))
		return errs.Err
	default:
		return errOutputType
	}
}

// For each address in [addrs], persist that the UTXO whose ID is [utxoID]
// has been spent and can no longer be spent by the address
func (s *prefixedState) removeUTXO(addrs []ids.ShortID, utxoID ids.ID) error {
	for _, addr := range addrs {
		addrID := addr.LongID()
		utxos := ids.Set{} // IDs of unspent UTXOs referencing [addr]
		if funds, err := s.Funds(addrID); err == nil {
			utxos.Add(funds...)
		}
		utxos.Remove(utxoID)                                     // Remove [utxoID] from this set
		if err := s.SetFunds(addrID, utxos.List()); err != nil { // Persist
			return err
		}
	}
	return nil
}

// FundUTXO persists [utxo].
// For each address referenced in [utxo]'s output, persists
// that the address is referenced by [utxo]
func (s *prefixedState) FundUTXO(utxo *UTXO) error {
	utxoID := utxo.ID()
	if err := s.SetUTXO(utxoID, utxo); err != nil {
		return err
	}

	switch out := utxo.Out().(type) {
	case *OutputPayment:
		return s.addUTXO(out.Addresses(), utxoID)
	case *OutputTakeOrLeave:
		errs := wrappers.Errs{}
		errs.Add(s.addUTXO(out.Addresses1(), utxoID))
		errs.Add(s.addUTXO(out.Addresses2(), utxoID))
		return errs.Err
	default:
		return errOutputType
	}
}

// Persist that each address in [addrs] is referenced in the UTXO whose ID is [utxoID]
func (s *prefixedState) addUTXO(addrs []ids.ShortID, utxoID ids.ID) error {
	for _, addr := range addrs {
		addrID := addr.LongID()
		utxos := ids.Set{}
		// Get the set of UTXO IDs such that [addr] is referenced in
		// the UTXO
		if funds, err := s.Funds(addrID); err == nil {
			utxos.Add(funds...)
		}
		// Add [utxoID] to that set
		utxos.Add(utxoID)
		// Persist the new set
		if err := s.SetFunds(addrID, utxos.List()); err != nil {
			return err
		}
	}
	return nil
}
