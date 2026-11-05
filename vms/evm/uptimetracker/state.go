// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/set"
)

const codecVersion uint16 = 0

var codecManager codec.Manager

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

var _ uptime.State = (*state)(nil)

// state holds the on-disk and cached representation of the validator set
type state struct {
	db database.Database

	validators             map[ids.ID]*validator
	nodeIDsToValidationIDs map[ids.NodeID]ids.ID
	dirtyValidationIDs     set.Set[ids.ID]
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

		s.addValidatorToMemory(validationID, vdr)
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

	s.dirtyValidationIDs.Add(v.validationID)

	return nil
}

func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	v, ok := s.getValidatorByNodeID(nodeID)
	if !ok {
		return time.Time{}, database.ErrNotFound
	}

	return time.Unix(int64(v.LastUpdated), 0), nil
}

// addNewValidator adds a new validator to the state and marks it for
// persistence to the database. This should be used when adding validators
// during runtime operations.
func (s *state) addNewValidator(vdr *validator) {
	s.addValidatorToMemory(vdr.validationID, vdr)
	s.dirtyValidationIDs.Add(vdr.validationID)
}

// updateValidator sets the isActive state of the validator with the given
// validationID -- this function assumes that a validator with the given
// validationID exists.
func (s *state) updateValidator(validationID ids.ID, isActive bool) {
	v := s.validators[validationID]
	if v.IsActive == isActive {
		return
	}

	v.IsActive = isActive
	s.dirtyValidationIDs.Add(validationID)
}

// deleteValidator deletes the validator with the given validationID -- this
// function assumes that a validator with the given validationID exists.
func (s *state) deleteValidator(validationID ids.ID) {
	v := s.validators[validationID]

	delete(s.validators, v.validationID)
	delete(s.nodeIDsToValidationIDs, v.NodeID)

	s.dirtyValidationIDs.Add(v.validationID)
}

// setValidators replaces the validator set with current and marks every
// changed validator for persistence.
func (s *state) setValidators(current map[ids.ID]*validators.GetCurrentValidatorOutput) {
	for validationID := range s.validators {
		if _, ok := current[validationID]; !ok {
			s.deleteValidator(validationID)
		}
	}

	for validationID, newValidator := range current {
		if s.hasValidationID(validationID) {
			s.updateValidator(validationID, newValidator.IsActive)
			continue
		}

		s.addNewValidator(&validator{
			NodeID:        newValidator.NodeID,
			validationID:  validationID,
			IsActive:      newValidator.IsActive,
			StartTime:     newValidator.StartTime,
			UpDuration:    0,
			LastUpdated:   newValidator.StartTime,
			IsL1Validator: newValidator.IsL1Validator,
			Weight:        newValidator.Weight,
		})
	}
}

func (s *state) writeModifications() error {
	batch := s.db.NewBatch()

	for validationID := range s.dirtyValidationIDs {
		v, ok := s.validators[validationID]
		if !ok {
			if err := batch.Delete(validationID[:]); err != nil {
				return fmt.Errorf("failed to delete validator: %w", err)
			}
			continue
		}

		validatorBytes, err := codecManager.Marshal(codecVersion, v)
		if err != nil {
			return fmt.Errorf("failed to marshal validator: %w", err)
		}

		if err := batch.Put(validationID[:], validatorBytes); err != nil {
			return fmt.Errorf("failed to put validator: %w", err)
		}
	}

	if err := batch.Write(); err != nil {
		return fmt.Errorf("failed to write batch: %w", err)
	}

	// We have written all pending updates
	clear(s.dirtyValidationIDs)

	return nil
}

// addValidatorToMemory adds a validator to the in-memory data structures only.
// This is used during initialization when loading from the database and does not
// mark the validator for persistence.
func (s *state) addValidatorToMemory(validationID ids.ID, validator *validator) {
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

// isInactiveValidator returns true if nodeID is a validator that is not active.
func (s *state) isInactiveValidator(nodeID ids.NodeID) bool {
	v, ok := s.getValidatorByNodeID(nodeID)
	return ok && !v.IsActive
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
