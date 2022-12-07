// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
)

func (cs *caminoState) UpdateDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	cs.modifiedDeposits[depositTxID] = deposit
}

func (cs *caminoState) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	// Try to get from modified state
	d, ok := cs.modifiedDeposits[depositTxID]
	// deposit was deleted
	if ok && d == nil {
		return nil, database.ErrNotFound
	}
	// Try to get it from cache
	if !ok {
		var depositIntf interface{}
		if depositIntf, ok = cs.depositsCache.Get(depositTxID); ok {
			d = depositIntf.(*deposit.Deposit)
		}
	}
	// Try to get it from database
	if !ok {
		depositBytes, err := cs.depositsDB.Get(depositTxID[:])
		if err != nil {
			return nil, err
		}

		d = &deposit.Deposit{}
		if _, err := blocks.GenesisCodec.Unmarshal(depositBytes, d); err != nil {
			return nil, err
		}

		cs.depositsCache.Put(depositTxID, d)
	}

	return d, nil
}

func (cs *caminoState) writeDeposits() error {
	for depositTxID, deposit := range cs.modifiedDeposits {
		delete(cs.modifiedDeposits, depositTxID)

		if deposit == nil {
			if err := cs.depositsDB.Delete(depositTxID[:]); err != nil {
				return err
			}
		} else {
			depositBytes, err := blocks.GenesisCodec.Marshal(blocks.Version, deposit)
			if err != nil {
				return fmt.Errorf("failed to serialize deposit: %w", err)
			}

			if err := cs.depositsDB.Put(depositTxID[:], depositBytes); err != nil {
				return err
			}
		}
	}
	return nil
}
