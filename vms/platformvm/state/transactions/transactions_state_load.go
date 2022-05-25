// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transactions

import (
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/signed"
	"github.com/ava-labs/avalanchego/vms/platformvm/transactions/unsigned"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

func (s *state) SyncGenesis(
	genesisUtxos []*avax.UTXO,
	genesisValidator []*signed.Tx,
	genesisChains []*signed.Tx,
) error {
	// Persist UTXOs that exist at genesis
	for _, utxo := range genesisUtxos {
		s.AddUTXO(utxo)
	}

	// Persist primary network validator set at genesis
	for _, vdrTx := range genesisValidator {
		tx, ok := vdrTx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		stakeAmount := tx.Validator.Wght
		stakeDuration := tx.Validator.Duration()
		currentSupply := s.GetCurrentSupply()

		r := s.rewards.Calculate(
			stakeDuration,
			stakeAmount,
			currentSupply,
		)
		newCurrentSupply, err := safemath.Add64(currentSupply, r)
		if err != nil {
			return err
		}

		s.AddCurrentStaker(vdrTx, r)
		s.AddTx(vdrTx, status.Committed)
		s.SetCurrentSupply(newCurrentSupply)
	}

	for _, chain := range genesisChains {
		unsignedChain, ok := chain.Unsigned.(*unsigned.CreateChainTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		// Ensure all chains that the genesis bytes say to create have the right
		// network ID
		if unsignedChain.NetworkID != s.ctx.NetworkID {
			return avax.ErrWrongNetworkID
		}

		s.AddChain(chain)
		s.AddTx(chain, status.Committed)
	}

	if err := s.WriteTxs(); err != nil {
		return err
	}

	return s.DataState.WriteMetadata()
}

func (s *state) LoadTxs() error {
	if err := s.LoadCurrentValidators(); err != nil {
		return err
	}
	return s.LoadPendingValidators()
}

func (s *state) LoadCurrentValidators() error {
	cs := &currentStaker{
		validatorsByNodeID: make(map[ids.NodeID]*currentValidatorImpl),
		validatorsByTxID:   make(map[ids.ID]*ValidatorReward),
	}

	validatorIt := s.currentValidatorList.NewIterator()
	defer validatorIt.Release()
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

		uptimeBytes := validatorIt.Value()
		uptime := &currentValidatorState{
			txID: txID,
		}
		if _, err := unsigned.Codec.Unmarshal(uptimeBytes, uptime); err != nil {
			return err
		}
		uptime.lastUpdated = time.Unix(int64(uptime.LastUpdated), 0)

		addValidatorTx, ok := tx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		cs.validatorsByNodeID[addValidatorTx.Validator.NodeID] = &currentValidatorImpl{
			validatorImpl: validatorImpl{
				subnets: make(map[ids.ID]signed.SubnetValidatorAndID),
			},
			addValidator: signed.ValidatorAndID{
				UnsignedAddValidatorTx: addValidatorTx,
				TxID:                   txID,
			},
			potentialReward: uptime.PotentialReward,
		}
		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx:     tx,
			PotentialReward: uptime.PotentialReward,
		}

		s.uptimes[addValidatorTx.Validator.NodeID] = uptime
	}

	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := s.currentDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		potentialRewardBytes := delegatorIt.Value()
		potentialReward, err := database.ParseUInt64(potentialRewardBytes)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.Unsigned.(*unsigned.AddDelegatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addDelegatorTx.Validator.NodeID]
		if !exists {
			return unsigned.ErrDelegatorSubset
		}
		vdr.delegatorWeight += addDelegatorTx.Validator.Wght
		vdr.delegators = append(vdr.delegators, signed.DelegatorAndID{
			UnsignedAddDelegatorTx: addDelegatorTx,
			TxID:                   txID,
		})
		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx:     tx,
			PotentialReward: potentialReward,
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := s.currentSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.Unsigned.(*unsigned.AddSubnetValidatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		cs.validators = append(cs.validators, tx)
		vdr, exists := cs.validatorsByNodeID[addSubnetValidatorTx.Validator.NodeID]
		if !exists {
			return unsigned.ErrDSValidatorSubset
		}
		vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = signed.SubnetValidatorAndID{
			UnsignedAddSubnetValidator: addSubnetValidatorTx,
			TxID:                       txID,
		}

		cs.validatorsByTxID[txID] = &ValidatorReward{
			AddStakerTx: tx,
		}
	}
	if err := subnetValidatorIt.Error(); err != nil {
		return err
	}

	for _, vdr := range cs.validatorsByNodeID {
		sortDelegatorsByRemoval(vdr.delegators)
	}
	sortValidatorsByRemoval(cs.validators)
	cs.SetNextStaker()

	s.SetCurrentStakerChainState(cs)
	return nil
}

func (s *state) LoadPendingValidators() error {
	ps := &pendingStaker{
		validatorsByNodeID:      make(map[ids.NodeID]signed.ValidatorAndID),
		validatorExtrasByNodeID: make(map[ids.NodeID]*validatorImpl),
	}

	validatorIt := s.pendingValidatorList.NewIterator()
	defer validatorIt.Release()
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

		addValidatorTx, ok := tx.Unsigned.(*unsigned.AddValidatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		ps.validatorsByNodeID[addValidatorTx.Validator.NodeID] = signed.ValidatorAndID{
			UnsignedAddValidatorTx: addValidatorTx,
			TxID:                   txID,
		}
	}
	if err := validatorIt.Error(); err != nil {
		return err
	}

	delegatorIt := s.pendingDelegatorList.NewIterator()
	defer delegatorIt.Release()
	for delegatorIt.Next() {
		txIDBytes := delegatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addDelegatorTx, ok := tx.Unsigned.(*unsigned.AddDelegatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID]; exists {
			vdr.delegators = append(vdr.delegators, signed.DelegatorAndID{
				UnsignedAddDelegatorTx: addDelegatorTx,
				TxID:                   txID,
			})
		} else {
			ps.validatorExtrasByNodeID[addDelegatorTx.Validator.NodeID] = &validatorImpl{
				delegators: []signed.DelegatorAndID{
					{
						UnsignedAddDelegatorTx: addDelegatorTx,
						TxID:                   txID,
					},
				},
				subnets: make(map[ids.ID]signed.SubnetValidatorAndID),
			}
		}
	}
	if err := delegatorIt.Error(); err != nil {
		return err
	}

	subnetValidatorIt := s.pendingSubnetValidatorList.NewIterator()
	defer subnetValidatorIt.Release()
	for subnetValidatorIt.Next() {
		txIDBytes := subnetValidatorIt.Key()
		txID, err := ids.ToID(txIDBytes)
		if err != nil {
			return err
		}
		tx, _, err := s.GetTx(txID)
		if err != nil {
			return err
		}

		addSubnetValidatorTx, ok := tx.Unsigned.(*unsigned.AddSubnetValidatorTx)
		if !ok {
			return unsigned.ErrWrongTxType
		}

		ps.validators = append(ps.validators, tx)
		if vdr, exists := ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID]; exists {
			vdr.subnets[addSubnetValidatorTx.Validator.Subnet] = signed.SubnetValidatorAndID{
				UnsignedAddSubnetValidator: addSubnetValidatorTx,
				TxID:                       txID,
			}
		} else {
			ps.validatorExtrasByNodeID[addSubnetValidatorTx.Validator.NodeID] = &validatorImpl{
				subnets: map[ids.ID]signed.SubnetValidatorAndID{
					addSubnetValidatorTx.Validator.Subnet: {
						UnsignedAddSubnetValidator: addSubnetValidatorTx,
						TxID:                       txID,
					},
				},
			}
		}
	}
	if err := subnetValidatorIt.Error(); err != nil {
		return err
	}

	for _, vdr := range ps.validatorExtrasByNodeID {
		sortDelegatorsByAddition(vdr.delegators)
	}
	sortValidatorsByAddition(ps.validators)

	s.SetPendingStakerChainState(ps)
	return nil
}
