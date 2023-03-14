// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

// TODO@ add tests
func (s *state) LockedUTXOs(txIDs set.Set[ids.ID], addresses set.Set[ids.ShortID], lockState locked.State) ([]*avax.UTXO, error) {
	retUtxos := []*avax.UTXO{}
	for address := range addresses {
		utxoIDs, err := s.UTXOIDs(address.Bytes(), ids.ID{}, math.MaxInt)
		if err != nil {
			return nil, err
		}
		for _, utxoID := range utxoIDs {
			utxo, err := s.GetUTXO(utxoID)
			if err != nil {
				return nil, err
			}
			if utxo == nil {
				continue
			}
			if lockedOut, ok := utxo.Out.(*locked.Out); ok &&
				lockedOut.IDs.Match(lockState, txIDs) {
				retUtxos = append(retUtxos, utxo)
			}
		}
	}
	return retUtxos, nil
}

func (s *state) Config() (*config.Config, error) {
	return s.cfg, nil
}

func (s *state) CaminoConfig() (*CaminoConfig, error) {
	return s.caminoState.CaminoConfig(), nil
}

func (s *state) SetAddressStates(address ids.ShortID, states uint64) {
	s.caminoState.SetAddressStates(address, states)
}

func (s *state) GetAddressStates(address ids.ShortID) (uint64, error) {
	return s.caminoState.GetAddressStates(address)
}

func (s *state) AddDepositOffer(offer *deposit.Offer) {
	s.caminoState.AddDepositOffer(offer)
}

func (s *state) GetDepositOffer(offerID ids.ID) (*deposit.Offer, error) {
	return s.caminoState.GetDepositOffer(offerID)
}

func (s *state) GetAllDepositOffers() ([]*deposit.Offer, error) {
	return s.caminoState.GetAllDepositOffers()
}

func (s *state) SetDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	s.caminoState.SetDeposit(depositTxID, deposit)
}

func (s *state) RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	s.caminoState.RemoveDeposit(depositTxID, deposit)
}

func (s *state) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	return s.caminoState.GetDeposit(depositTxID)
}

func (s *state) GetNextToUnlockDepositTime() (time.Time, error) {
	return s.caminoState.GetNextToUnlockDepositTime()
}

func (s *state) GetNextToUnlockDepositIDsAndTime() ([]ids.ID, time.Time, error) {
	return s.caminoState.GetNextToUnlockDepositIDsAndTime()
}

func (s *state) SetMultisigAlias(owner *multisig.Alias) {
	s.caminoState.SetMultisigAlias(owner)
}

func (s *state) GetMultisigAlias(alias ids.ShortID) (*multisig.Alias, error) {
	return s.caminoState.GetMultisigAlias(alias)
}

func (s *state) SetShortIDLink(id ids.ShortID, key ShortLinkKey, link *ids.ShortID) {
	s.caminoState.SetShortIDLink(id, key, link)
}

func (s *state) GetShortIDLink(id ids.ShortID, key ShortLinkKey) (ids.ShortID, error) {
	return s.caminoState.GetShortIDLink(id, key)
}

func (s *state) SetClaimable(ownerID ids.ID, claimable *Claimable) {
	s.caminoState.SetClaimable(ownerID, claimable)
}

func (s *state) GetClaimable(ownerID ids.ID) (*Claimable, error) {
	return s.caminoState.GetClaimable(ownerID)
}

func (s *state) SetNotDistributedValidatorReward(reward uint64) {
	s.caminoState.SetNotDistributedValidatorReward(reward)
}

func (s *state) GetNotDistributedValidatorReward() (uint64, error) {
	return s.caminoState.GetNotDistributedValidatorReward()
}

func (s *state) GetDeferredValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	return s.caminoState.GetDeferredValidator(subnetID, nodeID)
}

func (s *state) PutDeferredValidator(staker *Staker) {
	s.caminoState.PutDeferredValidator(staker)
}

func (s *state) DeleteDeferredValidator(staker *Staker) {
	s.caminoState.DeleteDeferredValidator(staker)
}

func (s *state) GetDeferredStakerIterator() (StakerIterator, error) {
	return s.caminoState.GetDeferredStakerIterator()
}
