// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

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
