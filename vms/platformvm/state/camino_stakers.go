// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"

	"github.com/ava-labs/avalanchego/database/linkeddb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func (cs *caminoState) GetDeferredValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return cs.deferredStakers.GetValidator(subnetID, nodeID)
}

func (cs *caminoState) PutDeferredValidator(staker *Staker) {
	cs.deferredStakers.PutValidator(staker)
}

func (cs *caminoState) DeleteDeferredValidator(staker *Staker) {
	cs.deferredStakers.DeleteValidator(staker)
}

func (cs *caminoState) GetDeferredStakerIterator() (StakerIterator, error) {
	return cs.deferredStakers.GetStakerIterator(), nil
}

func (cs *caminoState) loadDeferredValidators(s *state) error {
	cs.deferredStakers = newBaseStakers()

	validatorIt := cs.deferredValidatorList.NewIterator()
	defer validatorIt.Release()

	for _, validatorIt := range []database.Iterator{validatorIt} {
		for validatorIt.Next() {
			txIDBytes := validatorIt.Key()
			txID, err := ids.ToID(txIDBytes)
			if err != nil {
				return err
			}
			tx, _, err := s.GetTx(txID)
			if err != nil {
				return err
			}

			stakerTx, ok := tx.Unsigned.(txs.Staker)
			if !ok {
				return fmt.Errorf("expected tx type txs.Staker but got %T", tx.Unsigned)
			}

			staker, err := NewCurrentStaker(txID, stakerTx, 0)
			if err != nil {
				return err
			}

			validator := cs.deferredStakers.getOrCreateValidator(staker.SubnetID, staker.NodeID)
			validator.validator = staker

			cs.deferredStakers.stakers.ReplaceOrInsert(staker)
		}
	}

	return validatorIt.Error()
}

func (cs *caminoState) writeDeferredStakers() error {
	for subnetID, subnetValidatorDiffs := range cs.deferredStakers.validatorDiffs {
		delete(cs.deferredStakers.validatorDiffs, subnetID)

		validatorDB := cs.deferredValidatorList

		for _, validatorDiff := range subnetValidatorDiffs {
			err := writeDeferredDiff(
				validatorDB,
				validatorDiff,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func writeDeferredDiff(
	deferredValidatorList linkeddb.LinkedDB,
	validatorDiff *diffValidator,
) error {
	var err error
	switch validatorDiff.validatorStatus {
	case added:
		err = deferredValidatorList.Put(validatorDiff.validator.TxID[:], nil)
	case deleted:
		err = deferredValidatorList.Delete(validatorDiff.validator.TxID[:])
	}
	if err != nil {
		return fmt.Errorf("failed to update deferred validator: %w", err)
	}
	return nil
}
