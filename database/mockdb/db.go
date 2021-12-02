// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mockdb

import (
	"errors"

	"github.com/ava-labs/avalanchego/database"
)

var (
	errNoFunction = errors.New("user didn't specify what value(s) return")

	_ database.Database = &Database{}
)

// Database implements database.Database.
// This is a mock database meant to be used in tests.
// You specify the database's return value(s) for a given method call by
// assign value to the corresponding member.
// For example, to specify what should happen when Has is called,
// assign a value to OnHas.
// If no value is assigned to the corresponding member, the method returns an error or nil
// If you
type Database struct {
	// Executed when Has is called
	OnHas                           func([]byte) (bool, error)
	OnGet                           func([]byte) ([]byte, error)
	OnPut                           func([]byte, []byte) error
	OnDelete                        func([]byte) error
	OnNewBatch                      func() database.Batch
	OnNewIterator                   func() database.Iterator
	OnNewIteratorWithStart          func([]byte) database.Iterator
	OnNewIteratorWithPrefix         func([]byte) database.Iterator
	OnNewIteratorWithStartAndPrefix func([]byte, []byte) database.Iterator
	OnStat                          func(string) (string, error)
	OnCompact                       func([]byte, []byte) error
	OnClose                         func() error
}

// Has implements the database.Database interface
func (db *Database) Has(k []byte) (bool, error) {
	if db.OnHas == nil {
		return false, errNoFunction
	}
	return db.OnHas(k)
}

// Get implements the database.Database interface
func (db *Database) Get(k []byte) ([]byte, error) {
	if db.OnGet == nil {
		return nil, errNoFunction
	}
	return db.OnGet(k)
}

// Put implements the database.Database interface
func (db *Database) Put(k, v []byte) error {
	if db.OnPut == nil {
		return errNoFunction
	}
	return db.OnPut(k, v)
}

// Delete implements the database.Database interface
func (db *Database) Delete(k []byte) error {
	if db.OnDelete == nil {
		return errNoFunction
	}
	return db.OnDelete(k)
}

// NewBatch implements the database.Database interface
func (db *Database) NewBatch() database.Batch {
	if db.OnNewBatch == nil {
		return nil
	}
	return db.OnNewBatch()
}

// NewIterator implements the database.Database interface
func (db *Database) NewIterator() database.Iterator {
	if db.OnNewIterator == nil {
		return nil
	}
	return db.OnNewIterator()
}

// NewIteratorWithStart implements the database.Database interface
func (db *Database) NewIteratorWithStart(start []byte) database.Iterator {
	if db.OnNewIteratorWithStart == nil {
		return nil
	}
	return db.OnNewIteratorWithStart(start)
}

// NewIteratorWithPrefix implements the database.Database interface
func (db *Database) NewIteratorWithPrefix(prefix []byte) database.Iterator {
	if db.OnNewIteratorWithPrefix == nil {
		return nil
	}
	return db.OnNewIteratorWithPrefix(prefix)
}

// NewIteratorWithStartAndPrefix implements the database.Database interface
func (db *Database) NewIteratorWithStartAndPrefix(start, prefix []byte) database.Iterator {
	if db.OnNewIteratorWithStartAndPrefix == nil {
		return nil
	}
	return db.OnNewIteratorWithStartAndPrefix(start, prefix)
}

// Stat implements the database.Database interface
func (db *Database) Stat(stat string) (string, error) {
	if db.OnStat == nil {
		return "", errNoFunction
	}
	return db.OnStat(stat)
}

// Compact implements the database.Database interface
func (db *Database) Compact(start []byte, limit []byte) error {
	if db.OnCompact == nil {
		return errNoFunction
	}
	return db.OnCompact(start, limit)
}

// Close implements the database.Database interface
func (db *Database) Close() error {
	if db.OnClose == nil {
		return errNoFunction
	}
	return db.OnClose()
}

// New returns a new mock database
func New() *Database { return &Database{} }
