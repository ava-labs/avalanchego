// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
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
	Owner                *secp256k1fx.OutputOwners `serialize:"true"`
	ValidatorReward      uint64                    `serialize:"true"`
	ExpiredDepositReward uint64                    `serialize:"true"`
}

func (cs *caminoState) SetClaimable(ownerID ids.ID, claimable *Claimable) {
	cs.caminoDiff.modifiedClaimables[ownerID] = claimable
	cs.claimablesCache.Evict(ownerID)
}

func (cs *caminoState) GetClaimable(ownerID ids.ID) (*Claimable, error) {
	if claimable, ok := cs.modifiedClaimables[ownerID]; ok {
		if claimable == nil {
			return nil, database.ErrNotFound
		}
		return claimable, nil
	}

	if claimable, ok := cs.claimablesCache.Get(ownerID); ok {
		if claimable == nil {
			return nil, database.ErrNotFound
		}
		return claimable, nil
	}

	claimableBytes, err := cs.claimablesDB.Get(ownerID[:])
	if err == database.ErrNotFound {
		cs.claimablesCache.Put(ownerID, nil)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	claimable := &Claimable{}
	if _, err := blocks.GenesisCodec.Unmarshal(claimableBytes, claimable); err != nil {
		return nil, err
	}

	cs.claimablesCache.Put(ownerID, claimable)

	return claimable, nil
}

func (cs *caminoState) SetNotDistributedValidatorReward(reward uint64) {
	cs.modifiedNotDistributedValidatorReward = &reward
}

func (cs *caminoState) GetNotDistributedValidatorReward() (uint64, error) {
	if cs.modifiedNotDistributedValidatorReward != nil {
		return *cs.modifiedNotDistributedValidatorReward, nil
	}
	return cs.notDistributedValidatorReward, nil
}

func (cs *caminoState) writeClaimableAndValidatorRewards() error {
	if cs.modifiedNotDistributedValidatorReward != nil &&
		*cs.modifiedNotDistributedValidatorReward != cs.notDistributedValidatorReward {
		if err := database.PutUInt64(
			cs.caminoDB,
			notDistributedValidatorRewardKey,
			*cs.modifiedNotDistributedValidatorReward,
		); err != nil {
			return fmt.Errorf("failed to write notDistributedValidatorReward: %w", err)
		}
		cs.notDistributedValidatorReward = *cs.modifiedNotDistributedValidatorReward
	}
	cs.modifiedNotDistributedValidatorReward = nil

	for key, claimable := range cs.modifiedClaimables {
		delete(cs.modifiedClaimables, key)
		if claimable == nil {
			if err := cs.claimablesDB.Delete(key[:]); err != nil {
				return err
			}
		} else {
			claimableBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, claimable)
			if err != nil {
				return fmt.Errorf("failed to serialize claimable: %w", err)
			}
			if err := cs.claimablesDB.Put(key[:], claimableBytes); err != nil {
				return err
			}
		}
	}

	return nil
}

func (cs *caminoState) loadValidatorRewards() error {
	notDistributedValidatorReward, err := database.GetUInt64(cs.caminoDB, notDistributedValidatorRewardKey)
	if err == database.ErrNotFound {
		notDistributedValidatorReward = 0
	} else if err != nil {
		return err
	}
	cs.notDistributedValidatorReward = notDistributedValidatorReward
	return nil
}
