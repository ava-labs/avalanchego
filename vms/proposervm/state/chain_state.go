// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/ids"
)

const (
	lastAcceptedByte byte = iota
)

var (
	lastAcceptedKey = []byte{lastAcceptedByte}

	_ ChainState = &chainState{}
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
		return ids.ID{}, err
	}
	lastAccepted, err := ids.ToID(lastAcceptedBytes)
	if err != nil {
		return ids.ID{}, err
	}
	s.lastAccepted = lastAccepted
	return lastAccepted, nil
}
