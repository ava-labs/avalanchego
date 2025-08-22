// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/uptime"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/plugin/validators/state/interfaces"
)

var _ uptime.State = (*state)(nil)

type dbUpdateStatus bool

var (
	ErrAlreadyExists  = errors.New("validator already exists")
	ErrImmutableField = errors.New("immutable field cannot be updated")
)

const (
	updatedStatus dbUpdateStatus = true
	deletedStatus dbUpdateStatus = false
)

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

	listeners []interfaces.StateCallbackListener
}

// NewState creates a new State, it also loads the data from the disk
func NewState(db database.Database) (interfaces.State, error) {
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
	data, err := s.getData(nodeID)
	if err != nil {
		return 0, time.Time{}, err
	}
	return data.UpDuration, data.getLastUpdated(), nil
}

// SetUptime sets the uptime of the validator with the given nodeID
func (s *state) SetUptime(
	nodeID ids.NodeID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	data, err := s.getData(nodeID)
	if err != nil {
		return err
	}
	data.UpDuration = upDuration
	data.setLastUpdated(lastUpdated)

	s.updatedData[data.validationID] = updatedStatus
	return nil
}

// GetStartTime returns the start time of the validator with the given nodeID
func (s *state) GetStartTime(nodeID ids.NodeID) (time.Time, error) {
	data, err := s.getData(nodeID)
	if err != nil {
		return time.Time{}, err
	}
	return data.getStartTime(), nil
}

// AddValidator adds a new validator to the state
// the new validator is marked as updated and will be written to the disk when WriteState is called
func (s *state) AddValidator(vdr interfaces.Validator) error {
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

	for _, listener := range s.listeners {
		listener.OnValidatorAdded(vdr.ValidationID, vdr.NodeID, vdr.StartTimestamp, vdr.IsActive)
	}
	return nil
}

// UpdateValidator updates the validator in the state
// returns an error if the validator does not exist or if the immutable fields are modified
func (s *state) UpdateValidator(vdr interfaces.Validator) error {
	data, exists := s.data[vdr.ValidationID]
	if !exists {
		return database.ErrNotFound
	}
	// check immutable fields
	if !data.constantsAreUnmodified(vdr) {
		return ErrImmutableField
	}
	// check if mutable fields have changed
	updated := false
	if data.IsActive != vdr.IsActive {
		data.IsActive = vdr.IsActive
		updated = true
		for _, listener := range s.listeners {
			listener.OnValidatorStatusUpdated(data.validationID, data.NodeID, data.IsActive)
		}
	}

	if data.Weight != vdr.Weight {
		data.Weight = vdr.Weight
		updated = true
	}

	if updated {
		s.updatedData[vdr.ValidationID] = updatedStatus
	}
	return nil
}

// DeleteValidator marks the validator as deleted
// marked validator will be deleted from disk when WriteState is called
func (s *state) DeleteValidator(vID ids.ID) error {
	data, exists := s.data[vID]
	if !exists {
		return database.ErrNotFound
	}
	delete(s.data, data.validationID)
	delete(s.index, data.NodeID)

	// mark as deleted for WriteValidator
	s.updatedData[data.validationID] = deletedStatus

	for _, listener := range s.listeners {
		listener.OnValidatorRemoved(vID, data.NodeID)
	}
	return nil
}

// WriteState writes the updated state to the disk
func (s *state) WriteState() error {
	// TODO: consider adding batch size
	batch := s.db.NewBatch()
	for vID, updateStatus := range s.updatedData {
		switch updateStatus {
		case updatedStatus:
			data := s.data[vID]

			dataBytes, err := vdrCodec.Marshal(codecVersion, data)
			if err != nil {
				return err
			}
			if err := batch.Put(vID[:], dataBytes); err != nil {
				return err
			}
		case deletedStatus:
			if err := batch.Delete(vID[:]); err != nil {
				return err
			}
		}
	}
	if err := batch.Write(); err != nil {
		return err
	}
	// we've successfully flushed the updates, clear the updated marker.
	clear(s.updatedData)
	return nil
}

// SetStatus sets the active status of the validator with the given vID
func (s *state) SetStatus(vID ids.ID, isActive bool) error {
	data, exists := s.data[vID]
	if !exists {
		return database.ErrNotFound
	}
	data.IsActive = isActive
	s.updatedData[vID] = updatedStatus

	for _, listener := range s.listeners {
		listener.OnValidatorStatusUpdated(vID, data.NodeID, isActive)
	}
	return nil
}

// GetValidationIDs returns the validation IDs in the state
func (s *state) GetValidationIDs() set.Set[ids.ID] {
	ids := set.NewSet[ids.ID](len(s.data))
	for vID := range s.data {
		ids.Add(vID)
	}
	return ids
}

// GetNodeIDs returns the node IDs of validators in the state
func (s *state) GetNodeIDs() set.Set[ids.NodeID] {
	ids := set.NewSet[ids.NodeID](len(s.index))
	for nodeID := range s.index {
		ids.Add(nodeID)
	}
	return ids
}

// GetValidationID returns the validation ID for the given nodeID
func (s *state) GetValidationID(nodeID ids.NodeID) (ids.ID, error) {
	vID, exists := s.index[nodeID]
	if !exists {
		return ids.ID{}, database.ErrNotFound
	}
	return vID, nil
}

// GetValidator returns the validator data for the given validationID
func (s *state) GetValidator(vID ids.ID) (interfaces.Validator, error) {
	data, ok := s.data[vID]
	if !ok {
		return interfaces.Validator{}, database.ErrNotFound
	}
	return interfaces.Validator{
		ValidationID:   data.validationID,
		NodeID:         data.NodeID,
		StartTimestamp: data.StartTime,
		IsActive:       data.IsActive,
		Weight:         data.Weight,
		IsL1Validator:  data.IsL1Validator,
	}, nil
}

// RegisterListener registers a listener to the state
// OnValidatorAdded is called for all current validators on the provided listener before this function returns
func (s *state) RegisterListener(listener interfaces.StateCallbackListener) {
	s.listeners = append(s.listeners, listener)

	// notify the listener of the current state
	for vID, data := range s.data {
		listener.OnValidatorAdded(vID, data.NodeID, data.StartTime, data.IsActive)
	}
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
// returns database.ErrNotFound if the data does not exist
func (s *state) getData(nodeID ids.NodeID) (*validatorData, error) {
	vID, exists := s.index[nodeID]
	if !exists {
		return nil, database.ErrNotFound
	}
	data, exists := s.data[vID]
	if !exists {
		return nil, database.ErrNotFound
	}
	return data, nil
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
func (v *validatorData) constantsAreUnmodified(u interfaces.Validator) bool {
	return v.validationID == u.ValidationID &&
		v.NodeID == u.NodeID &&
		v.IsL1Validator == u.IsL1Validator &&
		v.StartTime == u.StartTimestamp
}
