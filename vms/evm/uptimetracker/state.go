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
	"github.com/ava-labs/avalanchego/utils/set"
)

const codecVersion uint16 = 0

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
	updatedValidators      set.Set[ids.ID]
	deletedValidators      set.Set[ids.ID]
}

func newState(db database.Database) (*state, error) {
	s := &state{
		db:                     db,
		validators:             make(map[ids.ID]*validator),
		nodeIDsToValidationIDs: make(map[ids.NodeID]ids.ID),
	}

	it := db.NewIterator()
	defer it.Release()

	for it.Next() {
		validationID, err := ids.ToID(it.Key())
		if err != nil {
			return nil, fmt.Errorf("failed to parse validation ID: %w", err)
		}

		vdr := &validator{
			validationID: validationID,
		}

		if _, err := codecManager.Unmarshal(it.Value(), vdr); err != nil {
			return nil, fmt.Errorf("failed to unmarshal validator: %w", err)
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

	s.updatedValidators.Add(v.validationID)

	return nil
}

func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	v, ok := s.getValidatorByNodeID(nodeID)
	if !ok {
		return time.Time{}, database.ErrNotFound
	}

	return time.Unix(int64(v.LastUpdated), 0), nil
}

func (s *state) addNewValidator(vdr *validator) {
	s.addValidator(vdr.validationID, vdr)
	s.updatedValidators.Add(vdr.validationID)
}

// updateValidator updates the validator with the given validationID to the
// given isActive state -- this function does assume that a validator with t
// he given validationID exists.
func (s *state) updateValidator(validationID ids.ID, isActive bool) bool {
	v := s.validators[validationID]

	if v.IsActive == isActive {
		return false
	}

	v.IsActive = isActive
	s.updatedValidators.Add(validationID)
	return true
}

// deleteValidator deletes the validator with the given validationID -- this function does assume that a validator with the given validationID exists.
// given isActive state -- this function does assume that a validator with t
// he given validationID exists.
func (s *state) deleteValidator(validationID ids.ID) bool {
	v := s.validators[validationID]

	delete(s.validators, v.validationID)
	delete(s.nodeIDsToValidationIDs, v.NodeID)

	s.deletedValidators.Add(v.validationID)

	return true
}

func (s *state) writeModifications() error {
	batch := s.db.NewBatch()

	for validationID := range s.updatedValidators {
		validatorBytes, err := codecManager.Marshal(
			codecVersion,
			s.validators[validationID],
		)
		if err != nil {
			return fmt.Errorf("failed to marshal validator: %w", err)
		}

		if err := batch.Put(validationID[:], validatorBytes); err != nil {
			return fmt.Errorf("failed to put validator: %w", err)
		}
	}

	for validationID := range s.deletedValidators {
		if err := batch.Delete(validationID[:]); err != nil {
			return fmt.Errorf("failed to delete validator: %w", err)
		}
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	// We have written all pending updates
	clear(s.updatedValidators)
	clear(s.deletedValidators)

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

	// we are guaranteed to have this validator
	v := s.validators[validationID]
	return v, true
}

func (s *state) hasValidationID(validationID ids.ID) bool {
	_, ok := s.validators[validationID]
	return ok
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
