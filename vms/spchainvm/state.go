// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package spchainvm

import (
	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/utils/wrappers"
)

// state is a thin wrapper around a database to provide, caching, serialization,
// and de-serialization.
type state struct{}

// Block attempts to load a block from storage.
func (s *state) Block(db database.Database, id ids.ID) (*Block, error) {
	bytes, err := db.Get(id.Bytes())
	if err != nil {
		return nil, err
	}

	// The key was in the database
	c := Codec{}
	return c.UnmarshalBlock(bytes)
}

// SetBlock saves the provided block to storage.
func (s *state) SetBlock(db database.Database, id ids.ID, block *Block) error {
	if block == nil {
		return db.Delete(id.Bytes())
	}
	return db.Put(id.Bytes(), block.bytes)
}

// Account attempts to load an account from storage.
func (s *state) Account(db database.Database, id ids.ID) (Account, error) {
	bytes, err := db.Get(id.Bytes())
	if err != nil {
		return Account{}, err
	}

	// The key was in the database
	codec := Codec{}
	return codec.UnmarshalAccount(bytes)
}

// SetAccount saves the provided account to storage.
func (s *state) SetAccount(db database.Database, id ids.ID, account Account) error {
	codec := Codec{}
	bytes, err := codec.MarshalAccount(account)
	if err != nil {
		return err
	}
	return db.Put(id.Bytes(), bytes)
}

// Status returns a status from storage.
func (s *state) Status(db database.Database, id ids.ID) (choices.Status, error) {
	bytes, err := db.Get(id.Bytes())
	if err != nil {
		return choices.Unknown, err
	}

	// The key was in the database
	p := wrappers.Packer{Bytes: bytes}
	status := choices.Status(p.UnpackInt())

	if p.Offset != len(bytes) {
		p.Add(errExtraSpace)
	}
	if p.Errored() {
		return choices.Unknown, p.Err
	}

	return status, nil
}

// SetStatus saves a status in storage.
func (s *state) SetStatus(db database.Database, id ids.ID, status choices.Status) error {
	if status == choices.Unknown {
		return db.Delete(id.Bytes())
	}

	p := wrappers.Packer{Bytes: make([]byte, 4)}

	p.PackInt(uint32(status))

	if p.Offset != len(p.Bytes) {
		p.Add(errExtraSpace)
	}
	if p.Errored() {
		return p.Err
	}

	return db.Put(id.Bytes(), p.Bytes)
}

// Alias returns an ID from storage.
func (s *state) Alias(db database.Database, id ids.ID) (ids.ID, error) {
	bytes, err := db.Get(id.Bytes())
	if err != nil {
		return ids.ID{}, err
	}

	// The key was in the database
	return ids.ToID(bytes)
}

// SetAlias saves an id in storage.
func (s *state) SetAlias(db database.Database, id ids.ID, alias ids.ID) error {
	if alias.IsZero() {
		return db.Delete(id.Bytes())
	}
	return db.Put(id.Bytes(), alias.Bytes())
}
