// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

var _ validatorUptimes = (*uptimes)(nil)

type uptimeAndReward struct {
	UpDuration      time.Duration `serialize:"true"`
	LastUpdated     uint64        `serialize:"true"` // Unix time in seconds
	PotentialReward uint64        `serialize:"true"`

	txID        ids.ID
	lastUpdated time.Time
}

type validatorUptimes interface {
	// LoadUptime sets the uptime measurements of [vdrID] on [subnetID] to
	// [uptime]. GetUptime and SetUptime will return an error if the [vdrID] and
	// [subnetID] hasn't been loaded. This call will not result in a write to disk.
	LoadUptime(
		vdrID ids.NodeID,
		subnetID ids.ID,
		uptime *uptimeAndReward,
	)

	// GetUptime returns the current uptime measurements of [vdrID] on
	// [subnetID].
	GetUptime(
		vdrID ids.NodeID,
		subnetID ids.ID,
	) (upDuration time.Duration, lastUpdated time.Time, err error)

	// SetUptime updates the uptime measurements of [vdrID] on [subnetID].
	// Unless these measurements are deleted first, the next call to
	// WriteUptimes will write this update to disk.
	SetUptime(
		vdrID ids.NodeID,
		subnetID ids.ID,
		upDuration time.Duration,
		lastUpdated time.Time,
	) error

	// DeleteUptime removes in-memory references to the uptimes measurements of
	// [vdrID] on [subnetID]. If there were staged updates from a prior call to
	// SetUptime, the updates will be dropped. This call will not result in a
	// write to disk.
	DeleteUptime(vdrID ids.NodeID, subnetID ids.ID)

	// WriteUptimes writes all staged updates from a prior call to SetUptime.
	WriteUptimes(
		dbPrimary database.KeyValueWriter,
		dbSubnet database.KeyValueWriter,
	) error
}

type uptimes struct {
	uptimes map[ids.NodeID]map[ids.ID]*uptimeAndReward // vdrID -> subnetID -> uptimes
	// updatedUptimes tracks the updates since the last call to WriteUptimes
	updatedUptimes map[ids.NodeID]set.Set[ids.ID] // vdrID -> subnetIDs
}

func newValidatorUptimes() validatorUptimes {
	return &uptimes{
		uptimes:        make(map[ids.NodeID]map[ids.ID]*uptimeAndReward),
		updatedUptimes: make(map[ids.NodeID]set.Set[ids.ID]),
	}
}

func (u *uptimes) LoadUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
	uptime *uptimeAndReward,
) {
	subnetUptimes, ok := u.uptimes[vdrID]
	if !ok {
		subnetUptimes = make(map[ids.ID]*uptimeAndReward)
		u.uptimes[vdrID] = subnetUptimes
	}
	subnetUptimes[subnetID] = uptime
}

func (u *uptimes) GetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
) (upDuration time.Duration, lastUpdated time.Time, err error) {
	uptime, exists := u.uptimes[vdrID][subnetID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return uptime.UpDuration, uptime.lastUpdated, nil
}

func (u *uptimes) SetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	uptime, exists := u.uptimes[vdrID][subnetID]
	if !exists {
		return database.ErrNotFound
	}
	uptime.UpDuration = upDuration
	uptime.lastUpdated = lastUpdated

	updatedSubnetUptimes, ok := u.updatedUptimes[vdrID]
	if !ok {
		updatedSubnetUptimes = set.Set[ids.ID]{}
		u.updatedUptimes[vdrID] = updatedSubnetUptimes
	}
	updatedSubnetUptimes.Add(subnetID)
	return nil
}

func (u *uptimes) DeleteUptime(vdrID ids.NodeID, subnetID ids.ID) {
	subnetUptimes := u.uptimes[vdrID]
	delete(subnetUptimes, subnetID)
	if len(subnetUptimes) == 0 {
		delete(u.uptimes, vdrID)
	}

	subnetUpdatedUptimes := u.updatedUptimes[vdrID]
	delete(subnetUpdatedUptimes, subnetID)
	if len(subnetUpdatedUptimes) == 0 {
		delete(u.updatedUptimes, vdrID)
	}
}

func (u *uptimes) WriteUptimes(
	dbPrimary database.KeyValueWriter,
	dbSubnet database.KeyValueWriter,
) error {
	for vdrID, updatedSubnets := range u.updatedUptimes {
		for subnetID := range updatedSubnets {
			uptime := u.uptimes[vdrID][subnetID]
			uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

			uptimeBytes, err := genesis.Codec.Marshal(txs.Version, uptime)
			if err != nil {
				return err
			}
			db := dbSubnet
			if subnetID == constants.PrimaryNetworkID {
				db = dbPrimary
			}
			if err := db.Put(uptime.txID[:], uptimeBytes); err != nil {
				return err
			}
		}
		delete(u.updatedUptimes, vdrID)
	}
	return nil
}
