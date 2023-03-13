// Copyright (C) 2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

func (cs *caminoState) SetDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.modifiedDeposits[depositTxID] = deposit
	cs.depositsCache.Evict(depositTxID)
}

func (cs *caminoState) RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.removedDeposits[depositTxID] = deposit
	cs.depositsCache.Evict(depositTxID)
}

// TODO@ are we really fine with deposit start time taken from new block chain time?

func (cs *caminoState) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	if _, ok := cs.removedDeposits[depositTxID]; ok {
		return nil, database.ErrNotFound
	}
	if deposit, ok := cs.modifiedDeposits[depositTxID]; ok {
		return deposit, nil
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
	for depositTxID, deposit := range cs.removedDeposits {
		delete(cs.removedDeposits, depositTxID)
		if err := cs.depositsDB.Delete(depositTxID[:]); err != nil {
			return err
		}
		if err := cs.depositIDsByEndtimeDB.Delete(depositToKey(depositTxID[:], deposit)); err != nil {
			return err
		}
	}
	for depositTxID, deposit := range cs.modifiedDeposits {
		delete(cs.modifiedDeposits, depositTxID)
		depositBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit)
		if err != nil {
			return fmt.Errorf("failed to serialize deposit: %w", err)
		}

		if err := cs.depositsDB.Put(depositTxID[:], depositBytes); err != nil {
			return err
		}

		if err := cs.depositIDsByEndtimeDB.Put(depositToKey(depositTxID[:], deposit), nil); err != nil {
			return err
		}
	}
	return nil
}

func (cs *caminoState) loadDeposits() error {
	cs.depositsNextToUnlockIDs = nil
	cs.depositsNextToUnlockTime = nil
	depositIterator := cs.depositIDsByEndtimeDB.NewIterator()
	defer depositIterator.Release()

	next := depositIterator.Next()
	if !next {
		return nil
	}

	depositID, depositEndtime, err := bytesToDepositIDAndEndtime(depositIterator.Key())
	if err != nil {
		return err
	}

	nextDepositsEndTimestamp := depositEndtime
	nextDeposits := []ids.ID{depositID}

	for depositIterator.Next() {
		depositID, depositEndtime, err := bytesToDepositIDAndEndtime(depositIterator.Key())
		if err != nil {
			return err
		}
		if depositEndtime > nextDepositsEndTimestamp { // we expect values to be sorted by endtime in ascending order
			break
		}
		nextDeposits = append(nextDeposits, depositID)
	}
	nextDepositsEndtime := time.Unix(int64(nextDepositsEndTimestamp), 0)
	cs.depositsNextToUnlockIDs = nextDeposits
	cs.depositsNextToUnlockTime = &nextDepositsEndtime
	return nil
}

// depositTxID must be ids.ID 32 bytes
func depositToKey(depositTxID []byte, deposit *deposit.Deposit) []byte {
	depositSortKey := make([]byte, 8+32)
	binary.LittleEndian.PutUint64(depositSortKey, uint64(deposit.EndTime().Unix()))
	copy(depositSortKey[8:], depositTxID)
	return depositSortKey
}

// depositTxID must be ids.ID 32 bytes
func bytesToDepositIDAndEndtime(depositSortKeyBytes []byte) (ids.ID, uint64, error) {
	depositID, err := ids.ToID(depositSortKeyBytes[8:])
	if err != nil {
		return ids.Empty, 0, err
	}
	return depositID, binary.LittleEndian.Uint64(depositSortKeyBytes[:8]), nil
}
