// Copyright (C) 2022-2023, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/multisig"
	"github.com/ava-labs/avalanchego/vms/platformvm/config"
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
		if utxo, exists := d.modifiedUTXOs[retUtxos[i].InputID()]; exists {
			if utxo.utxo == nil {
				retUtxos = append(retUtxos[:i], retUtxos[i+1:]...)
			} else {
				retUtxos[i] = utxo.utxo
			}
			delete(remaining, utxo.utxoID)
		}
	}

	// Step 2: Append new UTXOs
	for utxoID := range remaining {
		utxo := d.modifiedUTXOs[utxoID].utxo
		if utxo != nil {
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

func (d *diff) SetAddressStates(address ids.ShortID, states uint64) {
	d.caminoDiff.modifiedAddressStates[address] = states
}

func (d *diff) GetAddressStates(address ids.ShortID) (uint64, error) {
	if states, ok := d.caminoDiff.modifiedAddressStates[address]; ok {
		return states, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetAddressStates(address)
}

func (d *diff) AddDepositOffer(offer *deposit.Offer) {
	d.caminoDiff.modifiedDepositOffers[offer.ID] = offer
}

func (d *diff) GetDepositOffer(offerID ids.ID) (*deposit.Offer, error) {
	if offer, ok := d.caminoDiff.modifiedDepositOffers[offerID]; ok {
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

func (d *diff) UpdateDeposit(depositTxID ids.ID, deposit *deposit.Deposit) {
	d.caminoDiff.modifiedDeposits[depositTxID] = deposit
}

func (d *diff) GetDeposit(depositTxID ids.ID) (*deposit.Deposit, error) {
	if deposit, ok := d.caminoDiff.modifiedDeposits[depositTxID]; ok {
		return deposit, nil
	}

	parentState, ok := d.stateVersions.GetState(d.parentID)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrMissingParentState, d.parentID)
	}

	return parentState.GetDeposit(depositTxID)
}

func (d *diff) SetMultisigAlias(owner *multisig.Alias) {
	d.caminoDiff.modifiedMultisigOwners[owner.ID] = owner
}

func (d *diff) GetMultisigAlias(alias ids.ShortID) (*multisig.Alias, error) {
	if msigOwner, ok := d.caminoDiff.modifiedMultisigOwners[alias]; ok {
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

func (d *diff) SetLastRewardImportTimestamp(timestamp uint64) {
	d.caminoDiff.modifiedRewardImportTimestamp = &timestamp
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

// Finally apply all changes
func (d *diff) ApplyCaminoState(baseState State) {
	if d.caminoDiff.modifiedRewardImportTimestamp != nil {
		baseState.SetLastRewardImportTimestamp(*d.caminoDiff.modifiedRewardImportTimestamp)
	}

	if d.caminoDiff.modifiedNotDistributedValidatorReward != nil {
		baseState.SetNotDistributedValidatorReward(*d.caminoDiff.modifiedNotDistributedValidatorReward)
	}

	for k, v := range d.caminoDiff.modifiedAddressStates {
		baseState.SetAddressStates(k, v)
	}

	for _, depositOffer := range d.caminoDiff.modifiedDepositOffers {
		baseState.AddDepositOffer(depositOffer)
	}

	for depositTxID, deposit := range d.caminoDiff.modifiedDeposits {
		baseState.UpdateDeposit(depositTxID, deposit)
	}

	for _, v := range d.caminoDiff.modifiedMultisigOwners {
		baseState.SetMultisigAlias(v)
	}

	for fullKey, link := range d.caminoDiff.modifiedShortLinks {
		id, key := fromShortLinkKey(fullKey)
		baseState.SetShortIDLink(id, key, link)
	}

	for ownerID, claimable := range d.caminoDiff.modifiedClaimables {
		baseState.SetClaimable(ownerID, claimable)
	}
}
