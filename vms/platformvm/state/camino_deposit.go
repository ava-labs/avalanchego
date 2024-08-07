// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

type depositDiff struct {
	*deposit.Deposit
	added, removed bool
}

func (cs *caminoState) AddDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit, added: true}
}

func (cs *caminoState) ModifyDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit}
	cs.depositsCache.Evict(depositTxID)
}

func (cs *caminoState) RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit, removed: true}
	cs.depositsCache.Evict(depositTxID)
}

func (cs *caminoState) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	if depositDiff, ok := cs.modifiedDeposits[depositTxID]; ok {
		if depositDiff.removed {
			return nil, database.ErrNotFound
		}
		return depositDiff.Deposit, nil
	}

	if deposit, ok := cs.depositsCache.Get(depositTxID); ok {
		if deposit == nil {
			return nil, database.ErrNotFound
		}
		return deposit, nil
	}

	depositBytes, err := cs.depositsDB.Get(depositTxID[:])
	if err == database.ErrNotFound {
		cs.depositsCache.Put(depositTxID, nil)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	d := &deposit.Deposit{}
	if _, err := blocks.GenesisCodec.Unmarshal(depositBytes, d); err != nil {
		return nil, err
	}

	cs.depositsCache.Put(depositTxID, d)

	return d, nil
}

func (cs *caminoState) GetNextToUnlockDepositTime(removedDepositIDs set.Set[ids.ID]) (time.Time, error) {
	if cs.depositsNextToUnlockTime == nil {
		return mockable.MaxTime, database.ErrNotFound
	}

	for _, depositID := range cs.depositsNextToUnlockIDs {
		if !removedDepositIDs.Contains(depositID) {
			return *cs.depositsNextToUnlockTime, nil
		}
	}

	_, nextUnlockTime, err := cs.getNextToUnlockDepositIDsAndTimeFromDB(removedDepositIDs)
	return nextUnlockTime, err
}

func (cs *caminoState) GetNextToUnlockDepositIDsAndTime(removedDepositIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	if cs.depositsNextToUnlockTime == nil {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	var nextUnlockIDs []ids.ID
	for _, depositID := range cs.depositsNextToUnlockIDs {
		if !removedDepositIDs.Contains(depositID) {
			nextUnlockIDs = append(nextUnlockIDs, depositID)
		}
	}
	if len(nextUnlockIDs) > 0 {
		return nextUnlockIDs, *cs.depositsNextToUnlockTime, nil
	}

	return cs.getNextToUnlockDepositIDsAndTimeFromDB(removedDepositIDs)
}

func (cs *caminoState) writeDeposits() error {
	// checking if all current deposits were removed
	nextUnlockIDsIsEmpty := true
	for _, depositTxID := range cs.depositsNextToUnlockIDs {
		if depositDiff, ok := cs.modifiedDeposits[depositTxID]; !ok || !depositDiff.removed {
			nextUnlockIDsIsEmpty = false
			break
		}
	}

	// if not all current deposits were removed, we can try to update without peeking into db
	var nextUnlockIDs []ids.ID
	if !nextUnlockIDsIsEmpty {
		// calculating earliest next unlock time
		nextUnlockTime := *cs.depositsNextToUnlockTime
		for _, depositDiff := range cs.modifiedDeposits {
			if depositEndtime := depositDiff.EndTime(); depositDiff.added && depositEndtime.Before(nextUnlockTime) {
				nextUnlockTime = depositEndtime
			}
		}
		// adding current deposits
		if nextUnlockTime.Equal(*cs.depositsNextToUnlockTime) {
			for _, depositTxID := range cs.depositsNextToUnlockIDs {
				if depositDiff, ok := cs.modifiedDeposits[depositTxID]; !ok || !depositDiff.removed {
					nextUnlockIDs = append(nextUnlockIDs, depositTxID)
				}
			}
		}
		// adding new deposits
		needSort := false // depositIDs from db are already sorted
		for depositTxID, depositDiff := range cs.modifiedDeposits {
			if depositDiff.added && depositDiff.EndTime().Equal(nextUnlockTime) {
				nextUnlockIDs = append(nextUnlockIDs, depositTxID)
				needSort = true
			}
		}
		if needSort {
			utils.Sort(nextUnlockIDs)
		}
		cs.depositsNextToUnlockIDs = nextUnlockIDs
		cs.depositsNextToUnlockTime = &nextUnlockTime
	}

	// adding new deposits to db, deleting removed deposits from db
	for depositTxID, depositDiff := range cs.modifiedDeposits {
		delete(cs.modifiedDeposits, depositTxID)
		if depositDiff.removed {
			if err := cs.depositsDB.Delete(depositTxID[:]); err != nil {
				return err
			}
			if err := cs.depositIDsByEndtimeDB.Delete(depositToKey(depositTxID[:], depositDiff.Deposit)); err != nil {
				return err
			}
		} else {
			depositBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, depositDiff.Deposit)
			if err != nil {
				return fmt.Errorf("failed to serialize deposit: %w", err)
			}
			if err := cs.depositsDB.Put(depositTxID[:], depositBytes); err != nil {
				return err
			}

			if depositDiff.added {
				if err := cs.depositIDsByEndtimeDB.Put(depositToKey(depositTxID[:], depositDiff.Deposit), nil); err != nil {
					return err
				}
			}
		}
	}

	// getting earliest deposits from db if depositsNextToUnlockIDs is empty
	if len(nextUnlockIDs) == 0 {
		nextUnlockIDs, nextUnlockTime, err := cs.getNextToUnlockDepositIDsAndTimeFromDB(nil)
		switch {
		case err == database.ErrNotFound:
			cs.depositsNextToUnlockIDs = nil
			cs.depositsNextToUnlockTime = nil
		case err != nil:
			return err
		default:
			cs.depositsNextToUnlockIDs = nextUnlockIDs
			cs.depositsNextToUnlockTime = &nextUnlockTime
		}
	}
	return nil
}

func (cs *caminoState) loadDeposits() error {
	cs.depositsNextToUnlockIDs = nil
	cs.depositsNextToUnlockTime = nil
	depositsNextToUnlockIDs, depositsNextToUnlockTime, err := cs.getNextToUnlockDepositIDsAndTimeFromDB(nil)
	if err == database.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}
	cs.depositsNextToUnlockIDs = depositsNextToUnlockIDs
	cs.depositsNextToUnlockTime = &depositsNextToUnlockTime
	return nil
}

func (cs caminoState) getNextToUnlockDepositIDsAndTimeFromDB(removedDepositIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	depositIterator := cs.depositIDsByEndtimeDB.NewIterator()
	defer depositIterator.Release()

	var nextDeposits []ids.ID
	nextDepositsEndTimestamp := uint64(math.MaxUint64)

	for depositIterator.Next() {
		depositID, depositEndtime, err := bytesToDepositIDAndEndtime(depositIterator.Key())
		if err != nil {
			return nil, time.Time{}, err
		}

		if removedDepositIDs.Contains(depositID) {
			continue
		}

		// we expect values to be sorted by endtime in ascending order
		if depositEndtime > nextDepositsEndTimestamp {
			break
		}
		if depositEndtime < nextDepositsEndTimestamp {
			nextDepositsEndTimestamp = depositEndtime
		}
		nextDeposits = append(nextDeposits, depositID)
	}

	if err := depositIterator.Error(); err != nil {
		return nil, time.Time{}, err
	}

	if len(nextDeposits) == 0 {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	return nextDeposits, time.Unix(int64(nextDepositsEndTimestamp), 0), nil
}

// depositTxID must be ids.ID 32 bytes
func depositToKey(depositTxID []byte, deposit *deposit.Deposit) []byte {
	depositSortKey := make([]byte, 8+32)
	binary.BigEndian.PutUint64(depositSortKey, uint64(deposit.EndTime().Unix()))
	copy(depositSortKey[8:], depositTxID)
	return depositSortKey
}

// depositTxID must be ids.ID 32 bytes
func bytesToDepositIDAndEndtime(depositSortKeyBytes []byte) (ids.ID, uint64, error) {
	depositID, err := ids.ToID(depositSortKeyBytes[8:])
	if err != nil {
		return ids.Empty, 0, err
	}
	return depositID, binary.BigEndian.Uint64(depositSortKeyBytes[:8]), nil
}
