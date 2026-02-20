// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var errUncommittedChanges = errors.New("uncommitted validator changes exist")

// preDelegateeRewardSize is the size of codec marshalling
// [preDelegateeRewardMetadata].
//
// CodecVersionLen + UpDurationLen + LastUpdatedLen + PotentialRewardLen
const preDelegateeRewardSize = codec.VersionSize + 3*wrappers.LongLen

type preDelegateeRewardMetadata struct {
	UpDuration      time.Duration `v0:"true"`
	LastUpdated     uint64        `v0:"true"` // Unix time in seconds
	PotentialReward uint64        `v0:"true"`
}

type validatorMetadata struct {
	UpDuration               time.Duration `v0:"true"`
	LastUpdated              uint64        `v0:"true"` // Unix time in seconds
	PotentialReward          uint64        `v0:"true"`
	PotentialDelegateeReward uint64        `v0:"true"`
	StakerStartTime          uint64        `          v1:"true"`

	txID        ids.ID
	lastUpdated time.Time
}

// Permissioned validators originally wrote their values as nil.
// With Banff we wrote the potential reward.
// With Cortina we wrote the potential reward with the potential delegatee reward.
// We now write the uptime, reward, and delegatee reward together.
func parseValidatorMetadata(bytes []byte, metadata *validatorMetadata) error {
	switch len(bytes) {
	case 0:
	// nothing was stored

	case database.Uint64Size:
		// only potential reward was stored
		var err error
		metadata.PotentialReward, err = database.ParseUInt64(bytes)
		if err != nil {
			return err
		}

	case preDelegateeRewardSize:
		// potential reward and uptime was stored but potential delegatee reward
		// was not
		tmp := preDelegateeRewardMetadata{}
		if _, err := MetadataCodec.Unmarshal(bytes, &tmp); err != nil {
			return err
		}

		metadata.UpDuration = tmp.UpDuration
		metadata.LastUpdated = tmp.LastUpdated
		metadata.PotentialReward = tmp.PotentialReward
	default:
		// everything was stored
		if _, err := MetadataCodec.Unmarshal(bytes, metadata); err != nil {
			return err
		}
	}
	metadata.lastUpdated = time.Unix(int64(metadata.LastUpdated), 0)
	return nil
}

// ValidatorMutables holds mutable validator data that can be modified.
type ValidatorMutables struct {
	DelegateeReward uint64
}

func validatorMutablesFromMetadata(vdrMetadata *validatorMetadata) ValidatorMutables {
	return ValidatorMutables{
		DelegateeReward: vdrMetadata.PotentialDelegateeReward,
	}
}

type validatorStateKey struct {
	nodeID   ids.NodeID //nolint:unused // used as part of map key comparison
	subnetID ids.ID
}

type validatorState struct {
	primaryNetworkValidatorsDB database.KeyValueWriterDeleter
	subnetValidatorsDB         database.KeyValueWriterDeleter

	// metadata holds the validator data.
	metadata map[validatorStateKey]*validatorMetadata // (vdrID, subnetID) -> metadata

	// dirtyMetadata tracks which keys have been modified and need DB write on commit.
	dirtyMetadata map[validatorStateKey]struct{}

	// deleted tracks validators to be deleted from DB on commit, storing their txID.
	deleted map[validatorStateKey]ids.ID // (vdrID, subnetID) -> txID
}

// newValidatorState creates a new validatorState instance with the given database writers.
func newValidatorState(
	primaryNetworkValidatorsDB database.KeyValueWriterDeleter,
	subnetValidatorsDB database.KeyValueWriterDeleter,
) *validatorState {
	return &validatorState{
		metadata:                   make(map[validatorStateKey]*validatorMetadata),
		dirtyMetadata:              make(map[validatorStateKey]struct{}),
		deleted:                    make(map[validatorStateKey]ids.ID),
		primaryNetworkValidatorsDB: primaryNetworkValidatorsDB,
		subnetValidatorsDB:         subnetValidatorsDB,
	}
}

// LoadValidatorMetadata loads validator metadata into the in-memory state.
// Returns errUncommittedChanges if there are pending dirty changes for this validator.
func (m *validatorState) LoadValidatorMetadata(
	nodeID ids.NodeID,
	subnetID ids.ID,
	vdrMetadata *validatorMetadata,
) error {
	key := getValidatorStateKey(nodeID, subnetID)
	if _, ok := m.dirtyMetadata[key]; ok {
		return errUncommittedChanges
	}
	if _, ok := m.deleted[key]; ok {
		return errUncommittedChanges
	}

	m.metadata[key] = vdrMetadata
	return nil
}

// GetUptime returns the uptime for the Primary Network validator with [nodeID].
func (m *validatorState) GetUptime(nodeID ids.NodeID) (time.Duration, time.Time, error) {
	key := getValidatorStateKey(nodeID, constants.PrimaryNetworkID)
	vdrMetadata, err := m.getValidatorMetadata(key)
	if err != nil {
		return 0, time.Time{}, err
	}
	return vdrMetadata.UpDuration, vdrMetadata.lastUpdated, nil
}

// SetUptime sets the uptime for the Primary Network validator with [nodeID].
// The change is tracked as dirty and will be written on Commit.
func (m *validatorState) SetUptime(
	nodeID ids.NodeID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	key := getValidatorStateKey(nodeID, constants.PrimaryNetworkID)
	vdrMetadata, err := m.getValidatorMetadata(key)
	if err != nil {
		return err
	}

	vdrMetadata.UpDuration = upDuration
	vdrMetadata.LastUpdated = uint64(lastUpdated.Unix())
	vdrMetadata.lastUpdated = lastUpdated
	m.dirtyMetadata[key] = struct{}{}
	return nil
}

// GetValidatorMutables returns the mutable data for the validator on [subnetID] with [nodeID].
func (m *validatorState) GetValidatorMutables(nodeID ids.NodeID, subnetID ids.ID) (ValidatorMutables, error) {
	key := getValidatorStateKey(nodeID, subnetID)
	vdrMetadata, err := m.getValidatorMetadata(key)
	if err != nil {
		return ValidatorMutables{}, err
	}
	return validatorMutablesFromMetadata(vdrMetadata), nil
}

// SetValidatorMutables sets the mutable data for the validator on [subnetID] with [nodeID].
// The change is tracked as dirty and will be written on Commit.
func (m *validatorState) SetValidatorMutables(
	nodeID ids.NodeID,
	subnetID ids.ID,
	mutables ValidatorMutables,
) error {
	key := getValidatorStateKey(nodeID, subnetID)
	vdrMetadata, err := m.getValidatorMetadata(key)
	if err != nil {
		return err
	}

	vdrMetadata.PotentialDelegateeReward = mutables.DelegateeReward
	m.dirtyMetadata[key] = struct{}{}
	return nil
}

// DeleteValidatorMetadata marks the validator for deletion on [subnetID] with [nodeID].
// The deletion will be persisted to DB on Commit.
func (m *validatorState) DeleteValidatorMetadata(nodeID ids.NodeID, subnetID ids.ID) error {
	key := getValidatorStateKey(nodeID, subnetID)
	vdrMetadata, err := m.getValidatorMetadata(key)
	if err != nil {
		return err
	}

	m.deleted[key] = vdrMetadata.txID
	delete(m.metadata, key)
	delete(m.dirtyMetadata, key)
	return nil
}

// Commit writes all dirty validators to the database and deletes removed validators.
// Clears dirty tracking after successful commit.
func (m *validatorState) Commit(codecVersion uint16) error {
	// Write dirty metadata
	for key := range m.dirtyMetadata {
		vdrMetadata := m.metadata[key]

		db := m.subnetValidatorsDB
		if key.subnetID == constants.PrimaryNetworkID {
			db = m.primaryNetworkValidatorsDB
		}

		metadataBytes, err := MetadataCodec.Marshal(codecVersion, vdrMetadata)
		if err != nil {
			return err
		}
		if err := db.Put(vdrMetadata.txID[:], metadataBytes); err != nil {
			return err
		}
	}

	// Delete removed validators
	for key, txID := range m.deleted {
		db := m.subnetValidatorsDB
		if key.subnetID == constants.PrimaryNetworkID {
			db = m.primaryNetworkValidatorsDB
		}

		if err := db.Delete(txID[:]); err != nil {
			return err
		}
	}

	m.dirtyMetadata = make(map[validatorStateKey]struct{})
	m.deleted = make(map[validatorStateKey]ids.ID)
	return nil
}

func (m *validatorState) getValidatorMetadata(key validatorStateKey) (*validatorMetadata, error) {
	if _, ok := m.deleted[key]; ok {
		return nil, database.ErrNotFound
	}
	vdrMetadata, ok := m.metadata[key]
	if !ok {
		return nil, database.ErrNotFound
	}
	return vdrMetadata, nil
}

// getValidatorStateKey creates a composite key from nodeID and subnetID.
func getValidatorStateKey(nodeID ids.NodeID, subnetID ids.ID) validatorStateKey {
	return validatorStateKey{
		nodeID:   nodeID,
		subnetID: subnetID,
	}
}
