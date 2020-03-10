// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"github.com/ava-labs/gecko/cache"
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
)

const (
	blockID uint64 = iota
	accountID
	statusID
	lastAcceptedID
	dbInitializedID
)

var (
	lastAccepted  = ids.Empty.Prefix(lastAcceptedID)
	dbInitialized = ids.Empty.Prefix(dbInitializedID)
)

// prefixedState wraps a state object. By prefixing the state, there will be no
// collisions between different types of objects that have the same hash.
type prefixedState struct {
	state                  state
	block, account, status cache.Cacher
}

// Block attempts to load a block from storage.
func (s *prefixedState) Block(db database.Database, id ids.ID) (*Block, error) {
	return s.state.Block(db, s.uniqueID(id, blockID, s.block))
}

// SetBlock saves a block to the database
func (s *prefixedState) SetBlock(db database.Database, id ids.ID, block *Block) error {
	return s.state.SetBlock(db, s.uniqueID(id, blockID, s.block), block)
}

// Account attempts to load an account from storage.
func (s *prefixedState) Account(db database.Database, id ids.ID) (Account, error) {
	return s.state.Account(db, s.uniqueID(id, accountID, s.account))
}

// SetAccount saves an account to the database
func (s *prefixedState) SetAccount(db database.Database, id ids.ID, account Account) error {
	return s.state.SetAccount(db, s.uniqueID(id, accountID, s.account), account)
}

// Status returns the status of the provided block id from storage.
func (s *prefixedState) Status(db database.Database, id ids.ID) (choices.Status, error) {
	return s.state.Status(db, s.uniqueID(id, statusID, s.status))
}

// SetStatus saves the provided status to storage.
func (s *prefixedState) SetStatus(db database.Database, id ids.ID, status choices.Status) error {
	return s.state.SetStatus(db, s.uniqueID(id, statusID, s.status), status)
}

// LastAccepted returns the last accepted blockID from storage.
func (s *prefixedState) LastAccepted(db database.Database) (ids.ID, error) {
	return s.state.Alias(db, lastAccepted)
}

// SetLastAccepted saves the last accepted blockID to storage.
func (s *prefixedState) SetLastAccepted(db database.Database, id ids.ID) error {
	return s.state.SetAlias(db, lastAccepted, id)
}

// DBInitialized returns the status of this database. If the database is
// uninitialized, the status will be unknown.
func (s *prefixedState) DBInitialized(db database.Database) (choices.Status, error) {
	return s.state.Status(db, dbInitialized)
}

// SetDBInitialized saves the provided status of the database.
func (s *prefixedState) SetDBInitialized(db database.Database, status choices.Status) error {
	return s.state.SetStatus(db, dbInitialized, status)
}

func (s *prefixedState) uniqueID(id ids.ID, prefix uint64, cacher cache.Cacher) ids.ID {
	if cachedIDIntf, found := cacher.Get(id); found {
		return cachedIDIntf.(ids.ID)
	}
	uID := id.Prefix(prefix)
	cacher.Put(id, uID)
	return uID
}
