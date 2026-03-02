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
	// updatedMetadata tracks (vdrID, subnetID) -> txIDs needing DB sync since the
	// last WriteValidatorMetadata.
	updatedMetadata map[ids.NodeID]map[ids.ID]set.Set[ids.ID]
}

func newValidatorState() *validatorState {
	return &validatorState{
		metadata:        make(map[ids.NodeID]map[ids.ID]*validatorMetadata),
		updatedMetadata: make(map[ids.NodeID]map[ids.ID]set.Set[ids.ID]),
	}
}

// LoadValidatorMetadata sets the `uptime` of `vdrID` on `subnetID`.
// [GetUptime] and [SetUptime] will return an error if `vdrID` and
// `subnetID` hasn't been loaded. This call will not result in a write to
// disk.
func (vs *validatorState) LoadValidatorMetadata(
	vdrID ids.NodeID,
	subnetID ids.ID,
	uptime *validatorMetadata,
) {
	subnetMetadata, ok := vs.metadata[vdrID]
	if !ok {
		subnetMetadata = make(map[ids.ID]*validatorMetadata)
		vs.metadata[vdrID] = subnetMetadata
	}
	subnetMetadata[subnetID] = uptime
}

// AddValidatorMetadata loads the metadata and marks it as updated so it will
// be written to disk on the next call to [WriteValidatorMetadata].
func (vs *validatorState) AddValidatorMetadata(
	vdrID ids.NodeID,
	subnetID ids.ID,
	vm *validatorMetadata,
) {
	vs.LoadValidatorMetadata(vdrID, subnetID, vm)
	vs.addUpdatedTxID(vdrID, subnetID, vm.txID)
}

// GetUptime returns the current uptime measurements of `vdrID` on
// `subnetID`.
func (vs *validatorState) GetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
) (time.Duration, time.Time, error) {
	metadata, exists := vs.metadata[vdrID][subnetID]
	if !exists {
		return 0, time.Time{}, database.ErrNotFound
	}
	return metadata.UpDuration, metadata.lastUpdated, nil
}

// SetUptime updates the uptime measurements of `vdrID` on `subnetID`.
// Unless these measurements are deleted first, the next call to
// [WriteValidatorMetadata] will write this update to disk.
func (vs *validatorState) SetUptime(
	vdrID ids.NodeID,
	subnetID ids.ID,
	upDuration time.Duration,
	lastUpdated time.Time,
) error {
	metadata, exists := vs.metadata[vdrID][subnetID]
	if !exists {
		return database.ErrNotFound
	}
	metadata.UpDuration = upDuration
	metadata.lastUpdated = lastUpdated

	vs.addUpdatedTxID(vdrID, subnetID, metadata.txID)
	return nil
}

// GetDelegateeReward returns the current rewards accrued to `vdrID` on
// `subnetID`.
func (vs *validatorState) GetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
) (uint64, error) {
	metadata, exists := vs.metadata[vdrID][subnetID]
	if !exists {
		return 0, database.ErrNotFound
	}
	return metadata.PotentialDelegateeReward, nil
}

// SetDelegateeReward updates the rewards accrued to `vdrID` on `subnetID`.
// Unless these measurements are deleted first, the next call to
// [WriteValidatorMetadata] will write this update to disk.
func (vs *validatorState) SetDelegateeReward(
	subnetID ids.ID,
	vdrID ids.NodeID,
	amount uint64,
) error {
	metadata, exists := vs.metadata[vdrID][subnetID]
	if !exists {
		return database.ErrNotFound
	}
	metadata.PotentialDelegateeReward = amount

	vs.addUpdatedTxID(vdrID, subnetID, metadata.txID)
	return nil
}

// DeleteValidatorMetadata removes in-memory references to the metadata of
// `vdrID` on `subnetID`. The txID is recorded for deletion from disk on the
// next [WriteValidatorMetadata]. Any staged updates from [SetUptime] or
// [SetDelegateeReward] are dropped.
func (vs *validatorState) DeleteValidatorMetadata(vdrID ids.NodeID, subnetID ids.ID) {
	subnetMetadata := vs.metadata[vdrID]
	md, exists := subnetMetadata[subnetID]
	if exists {
		vs.addUpdatedTxID(vdrID, subnetID, md.txID)
	}

	delete(subnetMetadata, subnetID)
	if len(subnetMetadata) == 0 {
		delete(vs.metadata, vdrID)
	}
}

// WriteValidatorMetadata persists all entries in updatedMetadata to disk. For
// each (vdrID, subnetID) and txID in the set: if metadata exists and its txID
// matches, write it to disk; otherwise delete the txID from disk.
func (vs *validatorState) WriteValidatorMetadata(
	dbPrimary database.KeyValueWriterDeleter,
	dbSubnet database.KeyValueWriterDeleter,
	codecVersion uint16,
) error {
	for vdrID, bySubnet := range vs.updatedMetadata {
		for subnetID, txIDs := range bySubnet {
			db := dbSubnet
			if subnetID == constants.PrimaryNetworkID {
				db = dbPrimary
			}

			metadata, hasMetadata := vs.metadata[vdrID][subnetID]
			for txID := range txIDs {
				if !hasMetadata || txID != metadata.txID {
					if err := db.Delete(txID[:]); err != nil {
						return err
					}
				} else {
					metadata.LastUpdated = uint64(metadata.lastUpdated.Unix())
					metadataBytes, err := MetadataCodec.Marshal(codecVersion, metadata)
					if err != nil {
						return err
					}
					if err := db.Put(metadata.txID[:], metadataBytes); err != nil {
						return err
					}
				}
			}
		}
	}
	vs.updatedMetadata = make(map[ids.NodeID]map[ids.ID]set.Set[ids.ID])
	return nil
}

func (vs *validatorState) addUpdatedTxID(vdrID ids.NodeID, subnetID ids.ID, txID ids.ID) {
	subnet, ok := vs.updatedMetadata[vdrID]
	if !ok {
		subnet = make(map[ids.ID]set.Set[ids.ID])
		vs.updatedMetadata[vdrID] = subnet
	}
	txIDs, ok := subnet[subnetID]
	if !ok {
		txIDs = set.Set[ids.ID]{}
		subnet[subnetID] = txIDs
	}
	txIDs.Add(txID)
}
