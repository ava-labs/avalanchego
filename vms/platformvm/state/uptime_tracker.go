// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
)

type uptimeData struct {
	UpDuration  time.Duration `v0:"true"`
	LastUpdated uint64        `v0:"true"` // Unix time in seconds

	lastUpdated time.Time // in-memory convenience form
}

// UptimeTrackerState tracks primary network validators' uptime.
// Important: it's not thread-safe.
type UptimeTrackerState struct {
	validatorsUptime map[ids.NodeID]*uptimeData // can be nil, if deleted
	modified         map[ids.NodeID]struct{}
	db               database.Database
}

func NewUptimeTrackerState(db database.Database) *UptimeTrackerState {
	return &UptimeTrackerState{
		validatorsUptime: make(map[ids.NodeID]*uptimeData),
		modified:         make(map[ids.NodeID]struct{}),
		db:               db,
	}
}

func (u *UptimeTrackerState) GetUptime(nodeID ids.NodeID) (upDuration time.Duration, lastUpdated time.Time, err error) {
	if uptime, ok := u.validatorsUptime[nodeID]; ok && uptime != nil {
		return uptime.UpDuration, uptime.lastUpdated, nil
	}

	return 0, time.Time{}, database.ErrNotFound
}

func (u *UptimeTrackerState) Exists(nodeID ids.NodeID) bool {
	if uptime, ok := u.validatorsUptime[nodeID]; ok && uptime != nil {
		return true
	}

	return false
}

func (u *UptimeTrackerState) SetUptime(nodeID ids.NodeID, upDuration time.Duration, lastUpdated time.Time) {
	if uptime, ok := u.validatorsUptime[nodeID]; ok && uptime != nil {
		uptime.UpDuration = upDuration
		uptime.lastUpdated = lastUpdated
		u.modified[nodeID] = struct{}{}
		return
	}

	u.validatorsUptime[nodeID] = &uptimeData{
		UpDuration:  upDuration,
		lastUpdated: lastUpdated,
	}
	u.modified[nodeID] = struct{}{}
}

func (u *UptimeTrackerState) DeleteUptime(nodeID ids.NodeID) {
	u.validatorsUptime[nodeID] = nil
	u.modified[nodeID] = struct{}{}
}

func (u *UptimeTrackerState) LoadUptime(nodeID ids.NodeID) error {
	bytes, err := u.db.Get(nodeID[:])
	if err != nil {
		return err
	}

	uptime := &uptimeData{}
	if _, err := UptimeCodec.Unmarshal(bytes, uptime); err != nil {
		return err
	}
	uptime.lastUpdated = time.Unix(int64(uptime.LastUpdated), 0)

	u.validatorsUptime[nodeID] = uptime
	return nil
}

func (u *UptimeTrackerState) WriteUptime(codecVersion uint16) error {
	for nodeID := range u.modified {
		uptime := u.validatorsUptime[nodeID]
		if uptime == nil {
			if err := u.db.Delete(nodeID[:]); err != nil {
				return err
			}
			delete(u.validatorsUptime, nodeID)
			continue
		}

		uptime.LastUpdated = uint64(uptime.lastUpdated.Unix())

		bytes, err := UptimeCodec.Marshal(codecVersion, uptime)
		if err != nil {
			return err
		}

		if err := u.db.Put(nodeID[:], bytes); err != nil {
			return err
		}
	}

	clear(u.modified)
	return nil
}
