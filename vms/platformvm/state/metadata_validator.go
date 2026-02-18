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

func (s *state) loadValidatorMetadata(
	vdrID ids.NodeID,
	subnetID ids.ID,
	uptime *validatorMetadata,
) {
	subnetMetadata, ok := s.metadata[vdrID]
	if !ok {
		subnetMetadata = make(map[ids.ID]*validatorMetadata)
		s.metadata[vdrID] = subnetMetadata
	}
	subnetMetadata[subnetID] = uptime
}

func (s *state) GetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
) (uint64, error) {
	metadata, exists := s.metadata[vdrID][subnetID]
	if !exists {
		return 0, database.ErrNotFound
	}
	return metadata.PotentialDelegateeReward, nil
}

func (s *state) SetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
	amount uint64,
) error {
	if _, err := s.GetCurrentValidator(subnetID, vdrID); err != nil {
		return err
	}

	metadata, ok := s.metadata[vdrID][subnetID]
	if !ok {
		return database.ErrNotFound
	}
	metadata.PotentialDelegateeReward = amount

	s.addUpdatedMetadata(vdrID, subnetID)
	return nil
}

func (s *state) deleteValidatorMetadata(vdrID ids.NodeID, subnetID ids.ID) {
	subnetMetadata := s.metadata[vdrID]
	delete(subnetMetadata, subnetID)
	if len(subnetMetadata) == 0 {
		delete(s.metadata, vdrID)
	}

	subnetUpdatedMetadata := s.updatedMetadata[vdrID]
	subnetUpdatedMetadata.Remove(subnetID)
	if subnetUpdatedMetadata.Len() == 0 {
		delete(s.updatedMetadata, vdrID)
	}
}

func (s *state) writeValidatorMetadata(
	dbPrimary database.KeyValueWriter,
	dbSubnet database.KeyValueWriter,
	codecVersion uint16,
) error {
	for vdrID, updatedSubnets := range s.updatedMetadata {
		for subnetID := range updatedSubnets {
			metadata := s.metadata[vdrID][subnetID]
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
		delete(s.updatedMetadata, vdrID)
	}
	return nil
}

func (s *state) addUpdatedMetadata(vdrID ids.NodeID, subnetID ids.ID) {
	updatedSubnetMetadata, ok := s.updatedMetadata[vdrID]
	if !ok {
		updatedSubnetMetadata = set.Set[ids.ID]{}
		s.updatedMetadata[vdrID] = updatedSubnetMetadata
	}
	updatedSubnetMetadata.Add(subnetID)
}
