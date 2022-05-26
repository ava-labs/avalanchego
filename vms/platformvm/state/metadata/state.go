// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
)

var (
	_ DataState = &state{}

	singletonPrefix = []byte("singleton")

	lastAcceptedKey  = []byte("last accepted")
	initializedKey   = []byte("initialized")
	timestampKey     = []byte("timestamp")
	currentSupplyKey = []byte("current supply")
)

type DataState interface {
	Content
	Management
}

// Mutable interface collects all methods updating
// metadata state upon blocks execution
type Mutable interface {
	GetTimestamp() time.Time
	SetTimestamp(tm time.Time)
	GetCurrentSupply() uint64
	SetCurrentSupply(cs uint64)
}

// Content interface collects all methods to query and mutate
// all of the tracked metadata. Note this Content is a superset
// of Mutable
type Content interface {
	Mutable

	ShouldInit() (bool, error)
	DoneInit() error
	GetLastAccepted() ids.ID
	SetLastAccepted(ids.ID)
	SetHeight(height uint64)
	GetHeight() uint64
}

// Management interface collects all methods used to initialize
// metadata db upon vm initialization, along with methods to
// persist updated state.
type Management interface {
	// Upon vm initialization, SyncGenesis loads
	// metadata from genesis block as marshalled from bytes
	SyncGenesis(
		genesisBlkID ids.ID,
		genesisTimestamp uint64,
		genesisInitialSupply uint64,
	) error

	// Upon vm initialization, LoadMetadata pulls
	// metadata previously stored on disk
	LoadMetadata() error

	WriteMetadata() error
	CloseMetadata() error
}

func NewState(
	baseDB database.Database,
) DataState {
	return &state{
		singletonDB: prefixdb.New(singletonPrefix, baseDB),
	}
}

type state struct {
	singletonDB database.Database

	currentHeight                        uint64
	originalTimestamp, timestamp         time.Time
	originalCurrentSupply, currentSupply uint64
	originalLastAccepted, lastAccepted   ids.ID
}

func (ms *state) GetHeight() uint64                   { return ms.currentHeight }
func (ms *state) SetHeight(height uint64)             { ms.currentHeight = height }
func (ms *state) GetTimestamp() time.Time             { return ms.timestamp }
func (ms *state) SetTimestamp(tm time.Time)           { ms.timestamp = tm }
func (ms *state) GetCurrentSupply() uint64            { return ms.currentSupply }
func (ms *state) SetCurrentSupply(cs uint64)          { ms.currentSupply = cs }
func (ms *state) GetLastAccepted() ids.ID             { return ms.lastAccepted }
func (ms *state) SetLastAccepted(lastAccepted ids.ID) { ms.lastAccepted = lastAccepted }

func (ms *state) ShouldInit() (bool, error) {
	has, err := ms.singletonDB.Has(initializedKey)
	return !has, err
}

func (ms *state) DoneInit() error {
	return ms.singletonDB.Put(initializedKey, nil)
}

func (ms *state) SyncGenesis(
	genesisBlkID ids.ID,
	genesisTimestamp uint64,
	genesisInitialSupply uint64,
) error {
	genesisTime := time.Unix(int64(genesisTimestamp), 0)
	ms.SetTimestamp(genesisTime)
	ms.SetCurrentSupply(genesisInitialSupply)
	ms.SetLastAccepted(genesisBlkID)
	return ms.WriteMetadata()
}

func (ms *state) LoadMetadata() error {
	timestamp, err := database.GetTimestamp(ms.singletonDB, timestampKey)
	if err != nil {
		return err
	}
	ms.originalTimestamp = timestamp
	ms.SetTimestamp(timestamp)

	currentSupply, err := database.GetUInt64(ms.singletonDB, currentSupplyKey)
	if err != nil {
		return err
	}
	ms.originalCurrentSupply = currentSupply
	ms.SetCurrentSupply(currentSupply)

	lastAccepted, err := database.GetID(ms.singletonDB, lastAcceptedKey)
	if err != nil {
		return err
	}
	ms.originalLastAccepted = lastAccepted
	ms.lastAccepted = lastAccepted

	return nil
}

func (ms *state) WriteMetadata() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("failed to write singletons with: %w", err)
		}
	}()
	if !ms.originalTimestamp.Equal(ms.timestamp) {
		if err = database.PutTimestamp(ms.singletonDB, timestampKey, ms.timestamp); err != nil {
			return
		}
		ms.originalTimestamp = ms.timestamp
	}
	if ms.originalCurrentSupply != ms.currentSupply {
		if err = database.PutUInt64(ms.singletonDB, currentSupplyKey, ms.currentSupply); err != nil {
			return
		}
		ms.originalCurrentSupply = ms.currentSupply
	}
	if ms.originalLastAccepted != ms.lastAccepted {
		if err = database.PutID(ms.singletonDB, lastAcceptedKey, ms.lastAccepted); err != nil {
			return
		}
		ms.originalLastAccepted = ms.lastAccepted
	}
	return nil
}

func (ms *state) CloseMetadata() error {
	return ms.singletonDB.Close()
}
