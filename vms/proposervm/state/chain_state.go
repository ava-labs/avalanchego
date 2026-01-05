// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

const (
	lastAcceptedByte byte = iota
)

var (
	lastAcceptedKey = []byte{lastAcceptedByte}

	_ ChainState = (*chainState)(nil)
)

type ChainState interface {
	SetLastAccepted(blkID ids.ID) error
	DeleteLastAccepted() error
	GetLastAccepted() (ids.ID, error)
}

type chainState struct {
	lastAccepted ids.ID
	db           database.Database
}

func NewChainState(db database.Database) ChainState {
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
		return ids.Empty, err
	}
	lastAccepted, err := ids.ToID(lastAcceptedBytes)
	if err != nil {
		return ids.Empty, err
	}
	s.lastAccepted = lastAccepted
	return lastAccepted, nil
}
