// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"time"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

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

type validatorState struct {
	metadata map[ids.NodeID]map[ids.ID]*validatorMetadata // vdrID -> subnetID -> metadata
	// updatedMetadata tracks the updates since WriteValidatorMetadata was last called
	updatedMetadata map[ids.NodeID]set.Set[ids.ID] // vdrID -> subnetIDs
}

func newValidatorState() *validatorState {
	return &validatorState{
		metadata:        make(map[ids.NodeID]map[ids.ID]*validatorMetadata),
		updatedMetadata: make(map[ids.NodeID]set.Set[ids.ID]),
	}
}

func (m *validatorState) LoadValidatorMetadata(
	vdrID ids.NodeID,
	subnetID ids.ID,
	uptime *validatorMetadata,
) {
	subnetMetadata, ok := m.metadata[vdrID]
	if !ok {
		subnetMetadata = make(map[ids.ID]*validatorMetadata)
		m.metadata[vdrID] = subnetMetadata
	}
	subnetMetadata[subnetID] = uptime
}

func (m *validatorState) GetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
) (time.Duration, time.Time, error) {
	metadata, exists := m.metadata[vdrID][subnetID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return metadata.UpDuration, metadata.lastUpdated, nil
}

func (m *validatorState) SetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	metadata, exists := m.metadata[vdrID][subnetID]
	if !exists {
		return database.ErrNotFound
	}
	metadata.UpDuration = upDuration
	metadata.lastUpdated = lastUpdated

	m.addUpdatedMetadata(vdrID, subnetID)
	return nil
}

func (m *validatorState) GetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
) (uint64, error) {
	metadata, exists := m.metadata[vdrID][subnetID]
	if !exists {
		return 0, database.ErrNotFound
	}
	return metadata.PotentialDelegateeReward, nil
}

func (m *validatorState) SetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
	amount uint64,
) error {
	metadata, exists := m.metadata[vdrID][subnetID]
	if !exists {
		return database.ErrNotFound
	}
	metadata.PotentialDelegateeReward = amount

	m.addUpdatedMetadata(vdrID, subnetID)
	return nil
}

func (m *validatorState) DeleteValidatorMetadata(vdrID ids.NodeID, subnetID ids.ID) {
	subnetMetadata := m.metadata[vdrID]
	delete(subnetMetadata, subnetID)
	if len(subnetMetadata) == 0 {
		delete(m.metadata, vdrID)
	}

	subnetUpdatedMetadata := m.updatedMetadata[vdrID]
	subnetUpdatedMetadata.Remove(subnetID)
	if subnetUpdatedMetadata.Len() == 0 {
		delete(m.updatedMetadata, vdrID)
	}
}

func (m *validatorState) WriteValidatorMetadata(
	dbPrimary database.KeyValueWriter,
	dbSubnet database.KeyValueWriter,
	codecVersion uint16,
) error {
	for vdrID, updatedSubnets := range m.updatedMetadata {
		for subnetID := range updatedSubnets {
			metadata := m.metadata[vdrID][subnetID]
			metadata.LastUpdated = uint64(metadata.lastUpdated.Unix())

			metadataBytes, err := MetadataCodec.Marshal(codecVersion, metadata)
			if err != nil {
				return err
			}
			db := dbSubnet
			if subnetID == constants.PrimaryNetworkID {
				db = dbPrimary
			}
			if err := db.Put(metadata.txID[:], metadataBytes); err != nil {
				return err
			}
		}
		delete(m.updatedMetadata, vdrID)
	}
	return nil
}

func (m *validatorState) addUpdatedMetadata(vdrID ids.NodeID, subnetID ids.ID) {
	updatedSubnetMetadata, ok := m.updatedMetadata[vdrID]
	if !ok {
		updatedSubnetMetadata = set.Set[ids.ID]{}
		m.updatedMetadata[vdrID] = updatedSubnetMetadata
	}
	updatedSubnetMetadata.Add(subnetID)
}
