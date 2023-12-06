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
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

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

func (s *state) SetAddressStates(address ids.ShortID, states as.AddressState) {
	s.caminoState.SetAddressStates(address, states)
}

func (s *state) GetAddressStates(address ids.ShortID) (as.AddressState, error) {
	return s.caminoState.GetAddressStates(address)
}

func (s *state) SetDepositOffer(offer *deposit.Offer) {
	s.caminoState.SetDepositOffer(offer)
}

func (s *state) GetDepositOffer(offerID ids.ID) (*deposit.Offer, error) {
	return s.caminoState.GetDepositOffer(offerID)
}

func (s *state) GetAllDepositOffers() ([]*deposit.Offer, error) {
	return s.caminoState.GetAllDepositOffers()
}

func (s *state) AddDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	s.caminoState.AddDeposit(depositTxID, deposit)
}

func (s *state) ModifyDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	s.caminoState.ModifyDeposit(depositTxID, deposit)
}

func (s *state) RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	s.caminoState.RemoveDeposit(depositTxID, deposit)
}

func (s *state) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	return s.caminoState.GetDeposit(depositTxID)
}

func (s *state) GetNextToUnlockDepositTime(removedDepositIDs set.Set[ids.ID]) (time.Time, error) {
	return s.caminoState.GetNextToUnlockDepositTime(removedDepositIDs)
}

func (s *state) GetNextToUnlockDepositIDsAndTime(removedDepositIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	return s.caminoState.GetNextToUnlockDepositIDsAndTime(removedDepositIDs)
}

func (s *state) SetMultisigAlias(owner *multisig.AliasWithNonce) {
	s.caminoState.SetMultisigAlias(owner)
}

func (s *state) GetMultisigAlias(alias ids.ShortID) (*multisig.AliasWithNonce, error) {
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

func (s *state) AddProposal(proposalID ids.ID, proposal dac.ProposalState) {
	s.caminoState.AddProposal(proposalID, proposal)
}

func (s *state) ModifyProposal(proposalID ids.ID, proposal dac.ProposalState) {
	s.caminoState.ModifyProposal(proposalID, proposal)
}

func (s *state) RemoveProposal(proposalID ids.ID, proposal dac.ProposalState) {
	s.caminoState.RemoveProposal(proposalID, proposal)
}

func (s *state) GetProposal(proposalID ids.ID) (dac.ProposalState, error) {
	return s.caminoState.GetProposal(proposalID)
}

func (s *state) AddProposalIDToFinish(proposalID ids.ID) {
	s.caminoState.AddProposalIDToFinish(proposalID)
}

func (s *state) GetProposalIDsToFinish() ([]ids.ID, error) {
	return s.caminoState.GetProposalIDsToFinish()
}

func (s *state) RemoveProposalIDToFinish(proposalID ids.ID) {
	s.caminoState.RemoveProposalIDToFinish(proposalID)
}

func (s *state) GetNextToExpireProposalIDsAndTime(removedProposalIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	return s.caminoState.GetNextToExpireProposalIDsAndTime(removedProposalIDs)
}

func (s *state) GetNextProposalExpirationTime(removedProposalIDs set.Set[ids.ID]) (time.Time, error) {
	return s.caminoState.GetNextProposalExpirationTime(removedProposalIDs)
}

func (s *state) GetProposalIterator() (ProposalsIterator, error) {
	return s.caminoState.GetProposalIterator()
}

func (s *state) GetBaseFee() (uint64, error) {
	return s.caminoState.GetBaseFee()
}

func (s *state) SetBaseFee(baseFee uint64) {
	s.caminoState.SetBaseFee(baseFee)
}

func (s *state) GetFeeDistribution() ([dac.FeeDistributionFractionsCount]uint64, error) {
	return s.caminoState.GetFeeDistribution()
}

func (s *state) SetFeeDistribution(feeDistribution [dac.FeeDistributionFractionsCount]uint64) {
	s.caminoState.SetFeeDistribution(feeDistribution)
}
