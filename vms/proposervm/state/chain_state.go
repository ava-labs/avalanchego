// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	lastAcceptedByte byte = iota
	preferredByte
	verifiedByte
)

var (
	lastAcceptedKey            = []byte{lastAcceptedByte}
	preferredKey               = []byte{preferredByte}
	_               ChainState = (*chainState)(nil)
)

type ChainState interface {
	SetLastAccepted(blkID ids.ID) error
	DeleteLastAccepted() error
	GetLastAccepted() (ids.ID, error)

	PutVerifiedBlock(blkID ids.ID) error
	HasVerifiedBlock(blkID ids.ID) (bool, error)
	DeleteVerifiedBlock(blkID ids.ID) error
	SetPreference(preferredID ids.ID) error
	GetPreference() (ids.ID, error)
}

type chainState struct {
	lastAccepted ids.ID
	db           database.Database
}

func newChainState(db database.Database) *chainState {
	return &chainState{db: db}
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

func (s *chainState) PutVerifiedBlock(blkID ids.ID) error {
	return s.db.Put(verifiedBlockKey(blkID), nil)
}

func (s *chainState) HasVerifiedBlock(blkID ids.ID) (bool, error) {
	return s.db.Has(verifiedBlockKey(blkID))
}

func (s *chainState) DeleteVerifiedBlock(blkID ids.ID) error {
	return s.db.Delete(verifiedBlockKey(blkID))
}

func (s *chainState) SetPreference(preferredID ids.ID) error {
	return s.db.Put(preferredKey, preferredID[:])
}

func (s *chainState) GetPreference() (ids.ID, error) {
	bytes, err := s.db.Get(preferredKey)
	if err != nil {
		return ids.Empty, err
	}

	return ids.ToID(bytes)
}

func verifiedBlockKey(blkID ids.ID) []byte {
	preferredBlkIDKey := make([]byte, ids.IDLen+1)
	preferredBlkIDKey[0] = verifiedByte
	copy(preferredBlkIDKey[1:], blkID[:])
	return preferredBlkIDKey
}
