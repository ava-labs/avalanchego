// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

type Claimable struct {
	Owner           *secp256k1fx.OutputOwners `serialize:"true"`
	ValidatorReward uint64                    `serialize:"true"`
	DepositReward   uint64                    `serialize:"true"`
}

func (cs *caminoState) SetLastRewardImportTimestamp(timestamp uint64) {
	cs.caminoDiff.modifiedRewardImportTimestamp = &timestamp
}

func (cs *caminoState) SetClaimable(ownerID ids.ID, claimable *Claimable) {
	cs.caminoDiff.modifiedClaimables[ownerID] = claimable
}

func (cs *caminoState) GetClaimable(ownerID ids.ID) (*Claimable, error) {
	if claimable, ok := cs.modifiedClaimables[ownerID]; ok {
		return claimable, nil
	}

	claimableBytes, err := cs.depositsDB.Get(ownerID[:])
	if err != nil {
		return nil, err
	}

	claimable := &Claimable{}
	if _, err := blocks.GenesisCodec.Unmarshal(claimableBytes, claimable); err != nil {
		return nil, err
	}

	return claimable, nil
}

func (cs *caminoState) SetNotDistributedValidatorReward(reward uint64) {
	cs.modifiedNotDistributedValidatorReward = &reward
}

func (cs *caminoState) GetNotDistributedValidatorReward() (uint64, error) {
	if cs.modifiedNotDistributedValidatorReward != nil {
		return *cs.modifiedNotDistributedValidatorReward, nil
	}

	notDistributedValidatorReward, err := database.GetUInt64(cs.caminoDB, notDistributedValidatorRewardKey)
	switch err {
	case nil:
		return notDistributedValidatorReward, nil
	case database.ErrNotFound:
		return 0, nil
	default:
		return 0, err
	}
}

func (cs *caminoState) writeClaimableAndValidatorRewards() error {
	if cs.modifiedRewardImportTimestamp != nil {
		if err := database.PutUInt64(
			cs.caminoDB,
			lastRewardImportTimestampKey,
			*cs.modifiedRewardImportTimestamp,
		); err != nil {
			return fmt.Errorf("failed to write rewardImportTimestamp: %w", err)
		}
		cs.modifiedRewardImportTimestamp = nil
	}

	if cs.modifiedNotDistributedValidatorReward != nil {
		if err := database.PutUInt64(
			cs.caminoDB,
			notDistributedValidatorRewardKey,
			*cs.modifiedNotDistributedValidatorReward,
		); err != nil {
			return fmt.Errorf("failed to write notDistributedValidatorReward: %w", err)
		}
	}

	for key, claimable := range cs.modifiedClaimables {
		delete(cs.modifiedClaimables, key)
		if claimable == nil {
			if err := cs.claimableDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			claimableBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, claimable)
			if err != nil {
				return fmt.Errorf("failed to serialize claimable: %w", err)
			}
			if err := cs.claimableDB.Put(key[:], claimableBytes); err != nil {
				return err
			}
		}
	}

	return nil
}
