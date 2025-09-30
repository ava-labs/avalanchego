// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"fmt"
	"math"
	"time"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
)

const (
	codecVersion  uint16         = 0
	updatedStatus dbUpdateStatus = iota
	deletedStatus
)

var (
	codecManager codec.Manager
	_            uptime.State = (*state)(nil)
)

func init() {
	codecManager = codec.NewManager(math.MaxInt32)
	c := linearcodec.NewDefault()

	if err := c.RegisterType(validator{}); err != nil {
		panic(fmt.Errorf("failed to register type: %w", err))
	}

	if err := codecManager.RegisterCodec(codecVersion, c); err != nil {
		panic(fmt.Errorf("failed to register codec: %w", err))
	}
}

type dbUpdateStatus int

type validator struct {
	UpDuration    time.Duration `serialize:"true"`
	LastUpdated   uint64        `serialize:"true"`
	NodeID        ids.NodeID    `serialize:"true"`
	Weight        uint64        `serialize:"true"`
	StartTime     uint64        `serialize:"true"`
	IsActive      bool          `serialize:"true"`
	IsL1Validator bool          `serialize:"true"`

	validationID ids.ID
}

// state holds the on-disk and cached representation of the validator set
type state struct {
	db database.Database

	validators             map[ids.ID]*validator
	nodeIDsToValidationIDs map[ids.NodeID]ids.ID
	updates                map[ids.ID]dbUpdateStatus
}

func newState(db database.Database) (*state, error) {
	s := &state{
		db:                     db,
		validators:             make(map[ids.ID]*validator),
		nodeIDsToValidationIDs: make(map[ids.NodeID]ids.ID),
		updates:                make(map[ids.ID]dbUpdateStatus),
	}

	it := db.NewIterator()
	defer it.Release()

	for it.Next() {
		validationIDBytes := it.Key()
		validationID, err := ids.ToID(validationIDBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to parse validation ID: %w", err)
		}

		vdr := &validator{
			validationID: validationID,
		}

		if _, err := codecManager.Unmarshal(it.Value(), vdr); err != nil {
			return nil, fmt.Errorf("failed to unmarshal validator validator: %w", err)
		}

		s.addValidator(validationID, vdr)
	}

	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("failed to iterate: %w", err)
	}

	return s, nil
}

func (s *state) GetUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	v, ok := s.getValidatorByNodeID(nodeID)
	if !ok {
		return 0, time.Time{}, database.ErrNotFound
	}

	return v.UpDuration, time.Unix(int64(v.LastUpdated), 0), nil
}

func (s *state) SetUptime(
	nodeID ids.NodeID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	v, ok := s.getValidatorByNodeID(nodeID)
	if !ok {
		return database.ErrNotFound
	}

	v.UpDuration = upDuration
	v.LastUpdated = uint64(lastUpdated.Unix())

	s.updates[v.validationID] = updatedStatus

	return nil
}

func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	v, ok := s.getValidatorByNodeID(nodeID)
	if !ok {
		return time.Time{}, database.ErrNotFound
	}

	return time.Unix(int64(v.LastUpdated), 0), nil
}

func (s *state) addValidatorUpdate(vdr *validator) {
	s.addValidator(vdr.validationID, vdr)
	s.updates[vdr.validationID] = updatedStatus
}

func (s *state) updateValidator(
	validationID ids.ID,
	isActive bool,
	weight uint64,
) error {
	v, ok := s.validators[validationID]
	if !ok {
		return database.ErrNotFound
	}

	updated := deletedStatus
	if v.IsActive != isActive {
		v.IsActive = isActive
		updated = updatedStatus
	}

	if v.Weight != weight {
		v.Weight = weight
		updated = updatedStatus
	}

	s.updates[validationID] = updated

	return nil
}

func (s *state) deleteValidator(validationID ids.ID) bool {
	v, ok := s.validators[validationID]
	if !ok {
		return false
	}

	delete(s.validators, v.validationID)
	delete(s.nodeIDsToValidationIDs, v.NodeID)

	s.updates[v.validationID] = deletedStatus

	return true
}

func (s *state) writeState() error {
	batch := s.db.NewBatch()

	for validationID, updateStatus := range s.updates {
		switch updateStatus {
		case updatedStatus:
			v := s.validators[validationID]

			validatorBytes, err := codecManager.Marshal(codecVersion, v)
			if err != nil {
				return fmt.Errorf("failed to marshal validator: %w", err)
			}

			if err := batch.Put(validationID[:], validatorBytes); err != nil {
				return fmt.Errorf("failed to put validator: %w", err)
			}
		case deletedStatus:
			if err := batch.Delete(validationID[:]); err != nil {
				return fmt.Errorf("failed to delete validator: %w", err)
			}
		default:
			return fmt.Errorf("unknown update status: %v", updateStatus)
		}
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	// We have written all pending updates
	clear(s.updates)

	return nil
}

func (s *state) addValidator(validationID ids.ID, validator *validator) {
	s.validators[validationID] = validator
	s.nodeIDsToValidationIDs[validator.NodeID] = validationID
}

func (s *state) getValidatorByNodeID(nodeID ids.NodeID) (*validator, bool) {
	validationID, ok := s.nodeIDsToValidationIDs[nodeID]
	if !ok {
		return nil, false
	}

	v, ok := s.validators[validationID]
	if !ok {
		return nil, false
	}

	return v, true
}

func (s *state) getValidatorByValidationID(validationID ids.ID) (
	*validator,
	bool,
) {
	v, ok := s.validators[validationID]
	if !ok {
		return &validator{}, false
	}

	return v, true
}

func (s *state) getNodeID(validationID ids.ID) (ids.NodeID, bool) {
	v, ok := s.validators[validationID]
	if !ok {
		return ids.NodeID{}, false
	}

	return v.NodeID, true
}

func (s *state) getNodeIDs() []ids.NodeID {
	return maps.Keys(s.nodeIDsToValidationIDs)
}

func (s *state) getValidationIDs() []ids.ID {
	return maps.Keys(s.validators)
}
