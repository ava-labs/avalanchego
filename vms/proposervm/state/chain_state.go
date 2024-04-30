// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	lastAcceptedByte byte = iota
	preferredByte
	verifiedByte
)

var (
	lastAcceptedKey = []byte{lastAcceptedByte}
	preferredKey    = []byte{preferredByte}

	processingBlockDBPrefix = []byte("processing_block")

	_ ChainState = (*chainState)(nil)
)

type ChainState interface {
	SetLastAccepted(blkID ids.ID) error
	DeleteLastAccepted() error
	GetLastAccepted() (ids.ID, error)

	PutProcessingBlock(blkID ids.ID) error
	HasProcessingBlock(blkID ids.ID) (bool, error)
	DeleteProcessingBlock(blkID ids.ID) error
	SetPreference(preferredID ids.ID) error
	GetPreference() (ids.ID, error)
}

type chainState struct {
	lastAccepted ids.ID
	db           database.Database

	processingBlockDB database.Database
}

func newChainState(db database.Database) *chainState {
	return &chainState{
		db:                db,
		processingBlockDB: prefixdb.New(processingBlockDBPrefix, db),
	}
}

func (s *chainState) SetLastAccepted(blkID ids.ID) error {
	if s.lastAccepted == blkID {
		return nil
	}
	s.lastAccepted = blkID
	return s.db.Put(lastAcceptedKey, blkID[:])
}

func (s *chainState) DeleteLastAccepted() error {
	s.lastAccepted = ids.Empty
	return s.db.Delete(lastAcceptedKey)
}

func (s *chainState) GetLastAccepted() (ids.ID, error) {
	if s.lastAccepted != ids.Empty {
		return s.lastAccepted, nil
	}
	lastAcceptedBytes, err := s.db.Get(lastAcceptedKey)
	if err != nil {
		return ids.ID{}, err
	}
	lastAccepted, err := ids.ToID(lastAcceptedBytes)
	if err != nil {
		return ids.ID{}, err
	}
	s.lastAccepted = lastAccepted
	return lastAccepted, nil
}

func (s *chainState) PutProcessingBlock(blkID ids.ID) error {
	return s.processingBlockDB.Put(blkID[:], nil)
}

func (s *chainState) HasProcessingBlock(blkID ids.ID) (bool, error) {
	return s.processingBlockDB.Has(blkID[:])
}

func (s *chainState) DeleteProcessingBlock(blkID ids.ID) error {
	return s.processingBlockDB.Delete(blkID[:])
}

func (s *chainState) SetPreference(preferredID ids.ID) error {
	return database.PutID(s.db, preferredKey, preferredID)
}

func (s *chainState) GetPreference() (ids.ID, error) {
	return database.GetID(s.db, preferredKey)
}
