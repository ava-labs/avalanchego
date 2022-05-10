// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/database/versiondb"
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

func NewState(
	baseDB *versiondb.Database,
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
