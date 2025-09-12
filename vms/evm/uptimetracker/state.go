// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
)

var _ uptime.State = (*state)(nil)

type dbUpdateStatus int

const (
	updatedStatus dbUpdateStatus = iota
	deletedStatus
)

var (
	ErrAlreadyExists  = errors.New("validator already exists")
	ErrImmutableField = errors.New("immutable field cannot be updated")
)

// Validator represents a validator in the state
type Validator struct {
	ValidationID   ids.ID     `json:"validationID"`   // Unique validation identifier
	NodeID         ids.NodeID `json:"nodeID"`         // Node identifier
	Weight         uint64     `json:"weight"`         // Validator weight/stake
	StartTimestamp uint64     `json:"startTimestamp"` // When validation started
	IsActive       bool       `json:"isActive"`       // Whether validator is currently active
	IsL1Validator  bool       `json:"isL1Validator"`  // Whether this is an L1 validator
}

// The state implementation only allows existing validator's `weight` and `IsActive`
// fields to be updated; all other fields should be constant and if any other field
// changes, the state manager errors and does not update the validator.
//
// The state implementation also assumes NodeIDs are unique in the tracked set.
type validatorData struct {
	UpDuration    time.Duration `serialize:"true"`
	LastUpdated   uint64        `serialize:"true"`
	NodeID        ids.NodeID    `serialize:"true"`
	Weight        uint64        `serialize:"true"`
	StartTime     uint64        `serialize:"true"`
	IsActive      bool          `serialize:"true"`
	IsL1Validator bool          `serialize:"true"`

	validationID ids.ID // database key
}

type state struct {
	data  map[ids.ID]*validatorData // vID -> validatorData
	index map[ids.NodeID]ids.ID     // nodeID -> vID
	// updatedData tracks the updates since WriteValidator was last called
	updatedData map[ids.ID]dbUpdateStatus // vID -> updated status
	db          database.Database
}

// These methods are implemented and exported to satisfy the uptime.State interface
// NewState creates a new State, it also loads the data from the disk
func newState(db database.Database) (*state, error) {
	s := &state{
		index:       make(map[ids.NodeID]ids.ID),
		data:        make(map[ids.ID]*validatorData),
		updatedData: make(map[ids.ID]dbUpdateStatus),
		db:          db,
	}
	if err := s.loadFromDisk(); err != nil {
		return nil, fmt.Errorf("failed to load data from disk: %w", err)
	}
	return s, nil
}

// GetUptime returns the uptime of the validator with the given nodeID
func (s *state) GetUptime(
	nodeID ids.NodeID,
) (time.Duration, time.Time, error) {
	data, f := s.getData(nodeID)
	if !f {
		return 0, time.Time{}, database.ErrNotFound
	}
	return data.UpDuration, data.getLastUpdated(), nil
}

// SetUptime sets the uptime of the validator with the given nodeID
func (s *state) SetUptime(
	nodeID ids.NodeID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	data, f := s.getData(nodeID)
	if !f {
		return database.ErrNotFound
	}
	data.UpDuration = upDuration
	data.setLastUpdated(lastUpdated)

	s.updatedData[data.validationID] = updatedStatus
	return nil
}

// GetStartTime returns the start time of the validator with the given nodeID
func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	data, f := s.getData(nodeID)
	if !f {
		return time.Time{}, database.ErrNotFound
	}
	return data.getStartTime(), nil
}

// addValidator adds a new validator to the state
// the new validator is marked as updated and will be written to the disk when WriteState is called
func (s *state) addValidator(vdr Validator) error {
	data := &validatorData{
		NodeID:        vdr.NodeID,
		validationID:  vdr.ValidationID,
		IsActive:      vdr.IsActive,
		StartTime:     vdr.StartTimestamp,
		UpDuration:    0,
		LastUpdated:   vdr.StartTimestamp,
		IsL1Validator: vdr.IsL1Validator,
		Weight:        vdr.Weight,
	}
	if err := s.addData(vdr.ValidationID, data); err != nil {
		return err
	}

	s.updatedData[vdr.ValidationID] = updatedStatus
	return nil
}

// updateValidator updates the validator in the state
// returns an error if the validator does not exist or if the immutable fields are modified
func (s *state) updateValidator(vdr Validator) error {
	data, ok := s.data[vdr.ValidationID]
	if !ok {
		return database.ErrNotFound
	}
	// check immutable fields
	if !data.constantsAreUnmodified(vdr) {
		return ErrImmutableField
	}
	// check if mutable fields have changed
	updated := deletedStatus
	if data.IsActive != vdr.IsActive {
		data.IsActive = vdr.IsActive
		updated = updatedStatus
	}

	if data.Weight != vdr.Weight {
		data.Weight = vdr.Weight
		updated = updatedStatus
	}

	s.updatedData[vdr.ValidationID] = updated
	return nil
}

// deleteValidator marks the validator as deleted
// marked validator will be deleted from disk when WriteState is called
func (s *state) deleteValidator(vID ids.ID) bool {
	data, ok := s.data[vID]
	if !ok {
		return false
	}
	delete(s.data, data.validationID)
	delete(s.index, data.NodeID)

	// mark as deleted for WriteValidator
	s.updatedData[data.validationID] = deletedStatus
	return true
}

// writeState writes the updated state to the disk
func (s *state) writeState() bool {
	// TODO: consider adding batch size
	batch := s.db.NewBatch()
	for vID, updateStatus := range s.updatedData {
		switch updateStatus {
		case updatedStatus:
			data := s.data[vID]

			dataBytes, err := vdrCodec.Marshal(codecVersion, data)
			if err != nil {
				return false
			}
			if err := batch.Put(vID[:], dataBytes); err != nil {
				return false
			}
		case deletedStatus:
			if err := batch.Delete(vID[:]); err != nil {
				return false
			}
		}
	}
	if err := batch.Write(); err != nil {
		return false
	}
	// we've successfully flushed the updates, clear the updated marker.
	clear(s.updatedData)
	return true
}

// parseValidatorData parses the data from the bytes into given validatorData
func parseValidatorData(bytes []byte, data *validatorData) error {
	if len(bytes) != 0 {
		if _, err := vdrCodec.Unmarshal(bytes, data); err != nil {
			return err
		}
	}
	return nil
}

// Load the state from the disk
func (s *state) loadFromDisk() error {
	it := s.db.NewIterator()
	defer it.Release()
	for it.Next() {
		vIDBytes := it.Key()
		vID, err := ids.ToID(vIDBytes)
		if err != nil {
			return fmt.Errorf("failed to parse validator ID: %w", err)
		}
		vdr := &validatorData{
			validationID: vID,
		}
		if err := parseValidatorData(it.Value(), vdr); err != nil {
			return fmt.Errorf("failed to parse validator data: %w", err)
		}
		if err := s.addData(vID, vdr); err != nil {
			return err
		}
	}
	return it.Error()
}

// addData adds the data to the state
// returns an error if the data already exists
func (s *state) addData(vID ids.ID, data *validatorData) error {
	if _, exists := s.data[vID]; exists {
		return fmt.Errorf("%w, validationID: %s", ErrAlreadyExists, vID)
	}
	if _, exists := s.index[data.NodeID]; exists {
		return fmt.Errorf("%w, nodeID: %s", ErrAlreadyExists, data.NodeID)
	}

	s.data[vID] = data
	s.index[data.NodeID] = vID
	return nil
}

// getData returns the data for the validator with the given nodeID
// returns false if the data does not exist
func (s *state) getData(nodeID ids.NodeID) (*validatorData, bool) {
	vID, ok := s.index[nodeID]
	if !ok {
		return nil, false
	}
	data, ok := s.data[vID]
	if !ok {
		return nil, false
	}
	return data, true
}

func (v *validatorData) setLastUpdated(t time.Time) {
	v.LastUpdated = uint64(t.Unix())
}

func (v *validatorData) getLastUpdated() time.Time {
	return time.Unix(int64(v.LastUpdated), 0)
}

func (v *validatorData) getStartTime() time.Time {
	return time.Unix(int64(v.StartTime), 0)
}

// constantsAreUnmodified returns true if the constants of this validator have
// not been modified compared to the updated validator.
func (v *validatorData) constantsAreUnmodified(u Validator) bool {
	return v.validationID == u.ValidationID &&
		v.NodeID == u.NodeID &&
		v.IsL1Validator == u.IsL1Validator &&
		v.StartTime == u.StartTimestamp
}
