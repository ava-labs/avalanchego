// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"github.com/ava-labs/avalanchego/database"
)

const (
	IsInitializedKey byte = iota
)

var (
	isInitializedKey                = []byte{IsInitializedKey}
	_                SingletonState = &singletonState{}
)

// SingletonState is a thin wrapper around a database to provide, caching,
// serialization, and de-serialization of singletons.
type SingletonState interface {
	IsInitialized() (bool, error)
	SetInitialized() error
}

type singletonState struct {
	singletonDB database.Database
}

func NewSingletonState(db database.Database) SingletonState {
	return &singletonState{
		singletonDB: db,
	}
}

func (s *singletonState) IsInitialized() (bool, error) {
	return s.singletonDB.Has(isInitializedKey)
}

func (s *singletonState) SetInitialized() error {
	return s.singletonDB.Put(isInitializedKey, nil)
}
