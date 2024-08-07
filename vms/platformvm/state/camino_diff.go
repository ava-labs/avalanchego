// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	as "github.com/ava-labs/avalanchego/vms/platformvm/addrstate"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
	"github.com/ava-labs/avalanchego/vms/platformvm/deposit"
	"github.com/ava-labs/avalanchego/vms/platformvm/locked"
)

func NewCaminoDiff(
	parentID ids.ID,
	stateVersions Versions,
) (Diff, error) {
	parentState, ok := stateVersions.GetState(parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, parentID)
	}
	return &diff{
		parentID:      parentID,
		stateVersions: stateVersions,
		timestamp:     parentState.GetTimestamp(),
		caminoDiff:    newCaminoDiff(),
	}, nil
}

func (d *diff) LockedUTXOs(txIDs set.Set[ids.ID], addresses set.Set[ids.ShortID], lockState locked.State) ([]*avax.UTXO, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	retUtxos, err := parentState.LockedUTXOs(txIDs, addresses, lockState)
	if err != nil {
		return nil, err
	}

	// Apply modifiedUTXO's
	// Step 1: remove / update existing UTXOs
	remaining := set.NewSet[ids.ID](len(d.modifiedUTXOs))
	for k := range d.modifiedUTXOs {
		remaining.Add(k)
	}
	for i := len(retUtxos) - 1; i >= 0; i-- {
		utxoID := retUtxos[i].InputID()
		if utxo, exists := d.modifiedUTXOs[utxoID]; exists {
			if utxo == nil {
				retUtxos = append(retUtxos[:i], retUtxos[i+1:]...)
			} else {
				retUtxos[i] = utxo
			}
			delete(remaining, utxoID)
		}
	}

	// Step 2: Append new UTXOs
	for utxoID := range remaining {
		if utxo := d.modifiedUTXOs[utxoID]; utxo != nil {
			if lockedOut, ok := utxo.Out.(*locked.Out); ok &&
				lockedOut.IDs.Match(lockState, txIDs) {
				retUtxos = append(retUtxos, utxo)
			}
		}
	}

	return retUtxos, nil
}

func (d *diff) Config() (*config.Config, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.Config()
}

func (d *diff) CaminoConfig() (*CaminoConfig, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.CaminoConfig()
}

func (d *diff) SetAddressStates(address ids.ShortID, states as.AddressState) {
	d.caminoDiff.modifiedAddressStates[address] = states
}

func (d *diff) GetAddressStates(address ids.ShortID) (as.AddressState, error) {
	if states, ok := d.caminoDiff.modifiedAddressStates[address]; ok {
		return states, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetAddressStates(address)
}

func (d *diff) SetDepositOffer(offer *deposit.Offer) {
	d.caminoDiff.modifiedDepositOffers[offer.ID] = offer
}

func (d *diff) GetDepositOffer(offerID ids.ID) (*deposit.Offer, error) {
	if offer, ok := d.caminoDiff.modifiedDepositOffers[offerID]; ok {
		if offer == nil {
			return nil, database.ErrNotFound
		}
		return offer, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetDepositOffer(offerID)
}

func (d *diff) GetAllDepositOffers() ([]*deposit.Offer, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentOffers, err := parentState.GetAllDepositOffers()
	if err != nil {
		return nil, err
	}

	var offers []*deposit.Offer

	for _, offer := range d.caminoDiff.modifiedDepositOffers {
		if offer != nil {
			offers = append(offers, offer)
		}
	}

	for _, offer := range parentOffers {
		if _, ok := d.caminoDiff.modifiedDepositOffers[offer.ID]; !ok {
			offers = append(offers, offer)
		}
	}

	return offers, nil
}

func (d *diff) AddDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	d.caminoDiff.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit, added: true}
}

func (d *diff) ModifyDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	d.caminoDiff.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit}
}

func (d *diff) RemoveDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	d.caminoDiff.modifiedDeposits[depositTxID] = &depositDiff{Deposit: deposit, removed: true}
}

func (d *diff) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	if depositDiff, ok := d.caminoDiff.modifiedDeposits[depositTxID]; ok {
		if depositDiff.removed {
			return nil, database.ErrNotFound
		}
		return depositDiff.Deposit, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetDeposit(depositTxID)
}

func (d *diff) GetNextToUnlockDepositTime(removedDepositIDs set.Set[ids.ID]) (time.Time, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	for depositID, depositDiff := range d.caminoDiff.modifiedDeposits {
		if depositDiff.removed {
			removedDepositIDs.Add(depositID)
		}
	}

	nextUnlockTime, err := parentState.GetNextToUnlockDepositTime(removedDepositIDs)
	if err != nil && err != database.ErrNotFound {
		return time.Time{}, err
	}

	// calculating earliest unlock time from added deposits and parent unlock time
	for depositID, depositDiff := range d.caminoDiff.modifiedDeposits {
		depositEndtime := depositDiff.EndTime()
		if depositDiff.added && depositEndtime.Before(nextUnlockTime) && !removedDepositIDs.Contains(depositID) {
			nextUnlockTime = depositEndtime
		}
	}

	// no deposits
	if nextUnlockTime.Equal(mockable.MaxTime) {
		return mockable.MaxTime, database.ErrNotFound
	}

	return nextUnlockTime, nil
}

func (d *diff) GetNextToUnlockDepositIDsAndTime(removedDepositIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, time.Time{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	for depositID, depositDiff := range d.caminoDiff.modifiedDeposits {
		if depositDiff.removed {
			removedDepositIDs.Add(depositID)
		}
	}

	parentNextDepositIDs, parentNextUnlockTime, err := parentState.GetNextToUnlockDepositIDsAndTime(removedDepositIDs)
	if err != nil && err != database.ErrNotFound {
		return nil, time.Time{}, err
	}

	// calculating earliest unlock time from added deposits and parent unlock time
	nextUnlockTime := parentNextUnlockTime
	for depositID, depositDiff := range d.caminoDiff.modifiedDeposits {
		depositEndtime := depositDiff.EndTime()
		if depositDiff.added && depositEndtime.Before(nextUnlockTime) && !removedDepositIDs.Contains(depositID) {
			nextUnlockTime = depositEndtime
		}
	}

	// no deposits
	if nextUnlockTime.Equal(mockable.MaxTime) {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	var nextDepositIDs []ids.ID
	if !parentNextUnlockTime.After(nextUnlockTime) {
		nextDepositIDs = parentNextDepositIDs
	}

	// getting added deposits with endtime matching nextUnlockTime
	needSort := false
	for depositID, depositDiff := range d.caminoDiff.modifiedDeposits {
		if depositDiff.added && depositDiff.EndTime().Equal(nextUnlockTime) && !removedDepositIDs.Contains(depositID) {
			nextDepositIDs = append(nextDepositIDs, depositID)
			needSort = true
		}
	}

	if needSort {
		utils.Sort(nextDepositIDs)
	}

	return nextDepositIDs, nextUnlockTime, nil
}

func (d *diff) SetMultisigAlias(alias *multisig.AliasWithNonce) {
	d.caminoDiff.modifiedMultisigAliases[alias.ID] = alias
}

func (d *diff) GetMultisigAlias(alias ids.ShortID) (*multisig.AliasWithNonce, error) {
	if msigOwner, ok := d.caminoDiff.modifiedMultisigAliases[alias]; ok {
		if msigOwner == nil {
			return nil, database.ErrNotFound
		}
		return msigOwner, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetMultisigAlias(alias)
}

func (d *diff) SetShortIDLink(id ids.ShortID, key ShortLinkKey, link *ids.ShortID) {
	d.caminoDiff.modifiedShortLinks[toShortLinkKey(id, key)] = link
}

func (d *diff) GetShortIDLink(id ids.ShortID, key ShortLinkKey) (ids.ShortID, error) {
	if addr, ok := d.caminoDiff.modifiedShortLinks[toShortLinkKey(id, key)]; ok {
		if addr == nil {
			return ids.ShortEmpty, database.ErrNotFound
		}
		return *addr, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return ids.ShortEmpty, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetShortIDLink(id, key)
}

func (d *diff) SetClaimable(ownerID ids.ID, claimable *Claimable) {
	d.caminoDiff.modifiedClaimables[ownerID] = claimable
}

func (d *diff) GetClaimable(ownerID ids.ID) (*Claimable, error) {
	if claimable, ok := d.caminoDiff.modifiedClaimables[ownerID]; ok {
		if claimable == nil {
			return nil, database.ErrNotFound
		}
		return claimable, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetClaimable(ownerID)
}

func (d *diff) SetNotDistributedValidatorReward(reward uint64) {
	d.caminoDiff.modifiedNotDistributedValidatorReward = &reward
}

func (d *diff) GetNotDistributedValidatorReward() (uint64, error) {
	if d.caminoDiff.modifiedNotDistributedValidatorReward != nil {
		return *d.caminoDiff.modifiedNotDistributedValidatorReward, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetNotDistributedValidatorReward()
}

func (d *diff) GetDeferredValidator(subnetID ids.ID, nodeID ids.NodeID) (*Staker, error) {
	// If the validator was modified in this diff, return the modified
	// validator.
	newValidator, validatorDiffStatus := d.caminoDiff.deferredStakerDiffs.GetValidator(subnetID, nodeID)
	switch validatorDiffStatus {
	case added:
		return newValidator, nil
	case deleted:
		return nil, database.ErrNotFound
	}

	// If the validator wasn't modified in this diff, ask the parent state.
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}
	return parentState.GetDeferredValidator(subnetID, nodeID)
}

func (d *diff) PutDeferredValidator(staker *Staker) {
	d.caminoDiff.deferredStakerDiffs.PutValidator(staker)
}

func (d *diff) DeleteDeferredValidator(staker *Staker) {
	d.caminoDiff.deferredStakerDiffs.DeleteValidator(staker)
}

func (d *diff) GetDeferredStakerIterator() (StakerIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetDeferredStakerIterator()
	if err != nil {
		return nil, err
	}

	return d.caminoDiff.deferredStakerDiffs.GetStakerIterator(parentIterator), nil
}

func (d *diff) AddProposal(proposalID ids.ID, proposal dac.ProposalState) {
	d.caminoDiff.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal, added: true}
}

func (d *diff) ModifyProposal(proposalID ids.ID, proposal dac.ProposalState) {
	d.caminoDiff.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal}
}

func (d *diff) RemoveProposal(proposalID ids.ID, proposal dac.ProposalState) {
	d.caminoDiff.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal, removed: true}
}

func (d *diff) GetProposal(proposalID ids.ID) (dac.ProposalState, error) {
	if proposalDiff, ok := d.caminoDiff.modifiedProposals[proposalID]; ok {
		if proposalDiff.removed {
			return nil, database.ErrNotFound
		}
		return proposalDiff.Proposal, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetProposal(proposalID)
}

func (d *diff) AddProposalIDToFinish(proposalID ids.ID) {
	d.caminoDiff.modifiedProposalIDsToFinish[proposalID] = true
}

func (d *diff) RemoveProposalIDToFinish(proposalID ids.ID) {
	d.caminoDiff.modifiedProposalIDsToFinish[proposalID] = false
}

func (d *diff) GetProposalIDsToFinish() ([]ids.ID, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentProposalIDsToFinish, err := parentState.GetProposalIDsToFinish()
	if err != nil {
		return nil, err
	}

	if len(d.caminoDiff.modifiedProposalIDsToFinish) == 0 {
		return parentProposalIDsToFinish, nil
	}

	uniqueProposalIDsToFinish := set.Set[ids.ID]{}

	for _, proposalID := range parentProposalIDsToFinish {
		if notRemoved, ok := d.caminoDiff.modifiedProposalIDsToFinish[proposalID]; !ok || notRemoved {
			uniqueProposalIDsToFinish.Add(proposalID)
		}
	}
	for proposalID, added := range d.caminoDiff.modifiedProposalIDsToFinish {
		if added {
			uniqueProposalIDsToFinish.Add(proposalID)
		}
	}

	proposalIDsToFinish := uniqueProposalIDsToFinish.List()
	utils.Sort(proposalIDsToFinish)

	return proposalIDsToFinish, nil
}

func (d *diff) GetNextProposalExpirationTime(removedProposalIDs set.Set[ids.ID]) (time.Time, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		if proposalDiff.removed {
			removedProposalIDs.Add(proposalID)
		}
	}

	nextExpirationTime, err := parentState.GetNextProposalExpirationTime(removedProposalIDs)
	if err != nil && err != database.ErrNotFound {
		return time.Time{}, err
	}

	// calculating earliest expiration time from added proposals and parent expiration time
	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		proposalEndtime := proposalDiff.Proposal.EndTime()
		if proposalDiff.added && proposalEndtime.Before(nextExpirationTime) && !removedProposalIDs.Contains(proposalID) {
			nextExpirationTime = proposalEndtime
		}
	}

	// no proposals
	if nextExpirationTime.Equal(mockable.MaxTime) {
		return mockable.MaxTime, database.ErrNotFound
	}

	return nextExpirationTime, nil
}

func (d *diff) GetNextToExpireProposalIDsAndTime(removedProposalIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, time.Time{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		if proposalDiff.removed {
			removedProposalIDs.Add(proposalID)
		}
	}

	parentNextProposalIDs, parentNextExpirationTime, err := parentState.GetNextToExpireProposalIDsAndTime(removedProposalIDs)
	if err != nil && err != database.ErrNotFound {
		return nil, time.Time{}, err
	}

	// calculating earliest expiration time from added proposals and parent expiration time
	nextExpirationTime := parentNextExpirationTime
	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		proposalEndtime := proposalDiff.Proposal.EndTime()
		if proposalDiff.added && proposalEndtime.Before(nextExpirationTime) && !removedProposalIDs.Contains(proposalID) {
			nextExpirationTime = proposalEndtime
		}
	}

	// no proposals
	if nextExpirationTime.Equal(mockable.MaxTime) {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	var nextProposalIDs []ids.ID
	if !parentNextExpirationTime.After(nextExpirationTime) {
		nextProposalIDs = parentNextProposalIDs
	}

	// getting added proposals with endtime matching nextExpirationTime
	needSort := false
	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		if proposalDiff.added && proposalDiff.Proposal.EndTime().Equal(nextExpirationTime) && !removedProposalIDs.Contains(proposalID) {
			nextProposalIDs = append(nextProposalIDs, proposalID)
			needSort = true
		}
	}

	if needSort {
		utils.Sort(nextProposalIDs)
	}

	return nextProposalIDs, nextExpirationTime, nil
}

func (d *diff) GetProposalIterator() (ProposalsIterator, error) {
	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	parentIterator, err := parentState.GetProposalIterator()
	if err != nil {
		return nil, err
	}

	return &diffProposalsIterator{
		parentIterator:    parentIterator,
		modifiedProposals: d.caminoDiff.modifiedProposals,
	}, nil
}

var _ ProposalsIterator = (*diffProposalsIterator)(nil)

type diffProposalsIterator struct {
	parentIterator    ProposalsIterator
	modifiedProposals map[ids.ID]*proposalDiff
	err               error
}

func (it *diffProposalsIterator) Next() bool {
	for it.parentIterator.Next() {
		proposalID, err := it.parentIterator.key()
		if err != nil { // should never happen
			it.err = err
			return false
		}
		if proposalDiff, ok := it.modifiedProposals[proposalID]; !ok || !proposalDiff.removed {
			return true
		}
	}
	return false
}

func (it *diffProposalsIterator) Value() (dac.ProposalState, error) {
	proposalID, err := it.parentIterator.key()
	if err != nil { // should never happen
		return nil, err
	}
	if proposalDiff, ok := it.modifiedProposals[proposalID]; ok {
		return proposalDiff.Proposal, nil
	}
	return it.parentIterator.Value()
}

func (it *diffProposalsIterator) Error() error {
	parentIteratorErr := it.parentIterator.Error()
	switch {
	case parentIteratorErr != nil && it.err != nil:
		return fmt.Errorf("%w, %s", it.err, parentIteratorErr)
	case parentIteratorErr == nil && it.err != nil:
		return it.err
	case parentIteratorErr != nil && it.err == nil:
		return parentIteratorErr
	}
	return nil
}

func (it *diffProposalsIterator) Release() {
	it.parentIterator.Release()
}

func (it *diffProposalsIterator) key() (ids.ID, error) {
	return it.parentIterator.key() // err should never happen
}

func (d *diff) GetBaseFee() (uint64, error) {
	if d.caminoDiff.modifiedBaseFee != nil {
		return *d.caminoDiff.modifiedBaseFee, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetBaseFee()
}

func (d *diff) SetBaseFee(baseFee uint64) {
	d.caminoDiff.modifiedBaseFee = &baseFee
}

func (d *diff) GetFeeDistribution() ([dac.FeeDistributionFractionsCount]uint64, error) {
	if d.caminoDiff.modifiedFeeDistribution != nil {
		return *d.caminoDiff.modifiedFeeDistribution, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return [dac.FeeDistributionFractionsCount]uint64{}, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetFeeDistribution()
}

func (d *diff) SetFeeDistribution(feeDistribution [dac.FeeDistributionFractionsCount]uint64) {
	d.caminoDiff.modifiedFeeDistribution = &feeDistribution
}

// Finally apply all changes
func (d *diff) ApplyCaminoState(baseState State) error {
	if d.caminoDiff.modifiedNotDistributedValidatorReward != nil {
		baseState.SetNotDistributedValidatorReward(*d.caminoDiff.modifiedNotDistributedValidatorReward)
	}
	if d.caminoDiff.modifiedBaseFee != nil {
		baseState.SetBaseFee(*d.caminoDiff.modifiedBaseFee)
	}
	if d.caminoDiff.modifiedFeeDistribution != nil {
		baseState.SetFeeDistribution(*d.caminoDiff.modifiedFeeDistribution)
	}

	for k, v := range d.caminoDiff.modifiedAddressStates {
		baseState.SetAddressStates(k, v)
	}

	for _, depositOffer := range d.caminoDiff.modifiedDepositOffers {
		baseState.SetDepositOffer(depositOffer)
	}

	for depositTxID, depositDiff := range d.caminoDiff.modifiedDeposits {
		switch {
		case depositDiff.added:
			baseState.AddDeposit(depositTxID, depositDiff.Deposit)
		case depositDiff.removed:
			baseState.RemoveDeposit(depositTxID, depositDiff.Deposit)
		default:
			baseState.ModifyDeposit(depositTxID, depositDiff.Deposit)
		}
	}

	for _, v := range d.caminoDiff.modifiedMultisigAliases {
		baseState.SetMultisigAlias(v)
	}

	for fullKey, link := range d.caminoDiff.modifiedShortLinks {
		id, key := fromShortLinkKey(fullKey)
		baseState.SetShortIDLink(id, key, link)
	}

	for ownerID, claimable := range d.caminoDiff.modifiedClaimables {
		baseState.SetClaimable(ownerID, claimable)
	}

	for proposalID, proposalDiff := range d.caminoDiff.modifiedProposals {
		switch {
		case proposalDiff.added:
			baseState.AddProposal(proposalID, proposalDiff.Proposal)
		case proposalDiff.removed:
			baseState.RemoveProposal(proposalID, proposalDiff.Proposal)
		default:
			baseState.ModifyProposal(proposalID, proposalDiff.Proposal)
		}
	}

	for proposalID, added := range d.caminoDiff.modifiedProposalIDsToFinish {
		if added {
			baseState.AddProposalIDToFinish(proposalID)
		} else {
			baseState.RemoveProposalIDToFinish(proposalID)
		}
	}

	for _, validatorDiffs := range d.caminoDiff.deferredStakerDiffs.validatorDiffs {
		for _, validatorDiff := range validatorDiffs {
			switch validatorDiff.validatorStatus {
			case added:
				baseState.PutDeferredValidator(validatorDiff.validator)
			case deleted:
				baseState.DeleteDeferredValidator(validatorDiff.validator)
			}
		}
	}
	return nil
}
