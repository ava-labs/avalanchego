// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
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

	if depositIntf, ok := cs.depositsCache.Get(depositTxID); ok {
		return depositIntf.(*deposit.Deposit), nil
	}

	depositBytes, err := cs.depositsDB.Get(depositTxID[:])
	if err != nil {
		return nil, err
	}

	d := &deposit.Deposit{}
	if _, err := blocks.GenesisCodec.Unmarshal(depositBytes, d); err != nil {
		return nil, err
	}

	cs.depositsCache.Put(depositTxID, d)

	return d, nil
}

func (cs *caminoState) GetNextToUnlockDepositTime() (time.Time, error) {
	if cs.depositsNextToUnlockTime == nil {
		return time.Time{}, database.ErrNotFound
	}
	return *cs.depositsNextToUnlockTime, nil
}

func (cs *caminoState) GetNextToUnlockDepositIDsAndTime() ([]ids.ID, time.Time, error) {
	if cs.depositsNextToUnlockTime == nil {
		return nil, time.Time{}, database.ErrNotFound
	}
	return cs.depositsNextToUnlockIDs, *cs.depositsNextToUnlockTime, nil
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

	// adding new deposits to db
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
		nextUnlockIDs, nextUnlockTime, err := cs.getNextToUnlockDepositIDsAndTimeFromDB()
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
	depositsNextToUnlockIDs, depositsNextToUnlockTime, err := cs.getNextToUnlockDepositIDsAndTimeFromDB()
	if err == database.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}
	cs.depositsNextToUnlockIDs = depositsNextToUnlockIDs
	cs.depositsNextToUnlockTime = &depositsNextToUnlockTime
	return nil
}

func (cs caminoState) getNextToUnlockDepositIDsAndTimeFromDB() ([]ids.ID, time.Time, error) {
	depositIterator := cs.depositIDsByEndtimeDB.NewIterator()
	defer depositIterator.Release()

	next := depositIterator.Next()
	if !next {
		return nil, time.Time{}, database.ErrNotFound
	}

	depositID, depositEndtime, err := bytesToDepositIDAndEndtime(depositIterator.Key())
	if err != nil {
		return nil, time.Time{}, err
	}

	nextDepositsEndTimestamp := depositEndtime
	nextDeposits := []ids.ID{depositID}

	for depositIterator.Next() {
		depositID, depositEndtime, err := bytesToDepositIDAndEndtime(depositIterator.Key())
		if err != nil {
			return nil, time.Time{}, err
		}
		if depositEndtime > nextDepositsEndTimestamp { // we expect values to be sorted by endtime in ascending order
			break
		}
		nextDeposits = append(nextDeposits, depositID)
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
