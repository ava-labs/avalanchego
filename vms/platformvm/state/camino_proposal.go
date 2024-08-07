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
	"github.com/ava-labs/avalanchego/vms/platformvm/dac"
)

type proposalStateWrapper struct {
	dac.ProposalState `serialize:"true"`
}

type proposalDiff struct {
	Proposal       dac.ProposalState
	added, removed bool
}

func (cs *caminoState) AddProposal(proposalID ids.ID, proposal dac.ProposalState) {
	cs.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal, added: true}
}

func (cs *caminoState) ModifyProposal(proposalID ids.ID, proposal dac.ProposalState) {
	cs.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal}
	cs.proposalsCache.Evict(proposalID)
}

func (cs *caminoState) RemoveProposal(proposalID ids.ID, proposal dac.ProposalState) {
	cs.modifiedProposals[proposalID] = &proposalDiff{Proposal: proposal, removed: true}
	cs.proposalsCache.Evict(proposalID)
}

func (cs *caminoState) GetProposal(proposalID ids.ID) (dac.ProposalState, error) {
	if proposalDiff, ok := cs.modifiedProposals[proposalID]; ok {
		if proposalDiff.removed {
			return nil, database.ErrNotFound
		}
		return proposalDiff.Proposal, nil
	}

	if proposal, ok := cs.proposalsCache.Get(proposalID); ok {
		if proposal == nil {
			return nil, database.ErrNotFound
		}
		return proposal, nil
	}

	proposalBytes, err := cs.proposalsDB.Get(proposalID[:])
	if err == database.ErrNotFound {
		cs.proposalsCache.Put(proposalID, nil)
		return nil, err
	} else if err != nil {
		return nil, err
	}

	proposal := &proposalStateWrapper{}
	if _, err := dac.Codec.Unmarshal(proposalBytes, proposal); err != nil {
		return nil, err
	}

	cs.proposalsCache.Put(proposalID, proposal.ProposalState)

	return proposal.ProposalState, nil
}

func (cs *caminoState) AddProposalIDToFinish(proposalID ids.ID) {
	cs.modifiedProposalIDsToFinish[proposalID] = true
}

func (cs *caminoState) RemoveProposalIDToFinish(proposalID ids.ID) {
	cs.modifiedProposalIDsToFinish[proposalID] = false
}

func (cs *caminoState) GetProposalIDsToFinish() ([]ids.ID, error) {
	if len(cs.modifiedProposalIDsToFinish) == 0 {
		return cs.proposalIDsToFinish, nil
	}

	uniqueProposalIDsToFinish := set.Set[ids.ID]{}
	for _, proposalID := range cs.proposalIDsToFinish {
		if notRemoved, ok := cs.modifiedProposalIDsToFinish[proposalID]; !ok || notRemoved {
			uniqueProposalIDsToFinish.Add(proposalID)
		}
	}
	for proposalID, added := range cs.modifiedProposalIDsToFinish {
		if added {
			uniqueProposalIDsToFinish.Add(proposalID)
		}
	}

	proposalIDsToFinish := uniqueProposalIDsToFinish.List()
	utils.Sort(proposalIDsToFinish)
	return proposalIDsToFinish, nil
}

func (cs *caminoState) GetNextProposalExpirationTime(removedProposalIDs set.Set[ids.ID]) (time.Time, error) {
	if cs.proposalsNextExpirationTime == nil {
		return mockable.MaxTime, database.ErrNotFound
	}

	for _, proposalID := range cs.proposalsNextToExpireIDs {
		if !removedProposalIDs.Contains(proposalID) {
			return *cs.proposalsNextExpirationTime, nil
		}
	}

	_, nextExpirationTime, err := cs.getNextToExpireProposalIDsAndTimeFromDB(removedProposalIDs)
	return nextExpirationTime, err
}

func (cs *caminoState) GetNextToExpireProposalIDsAndTime(removedProposalIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	if cs.proposalsNextExpirationTime == nil {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	var nextToExpireIDs []ids.ID
	for _, proposalID := range cs.proposalsNextToExpireIDs {
		if !removedProposalIDs.Contains(proposalID) {
			nextToExpireIDs = append(nextToExpireIDs, proposalID)
		}
	}
	if len(nextToExpireIDs) > 0 {
		return nextToExpireIDs, *cs.proposalsNextExpirationTime, nil
	}

	return cs.getNextToExpireProposalIDsAndTimeFromDB(removedProposalIDs)
}

func (cs *caminoState) GetProposalIterator() (ProposalsIterator, error) {
	return &proposalsIterator{
		dbIterator:  cs.proposalsDB.NewIterator(),
		caminoState: cs,
	}, nil
}

func (cs *caminoState) writeProposals() error {
	// checking if all current proposals were removed
	nextIDsIsEmpty := true
	for _, proposalID := range cs.proposalsNextToExpireIDs {
		if proposalDiff, ok := cs.modifiedProposals[proposalID]; !ok || !proposalDiff.removed {
			nextIDsIsEmpty = false
			break
		}
	}

	// if not all current proposals were removed, we can try to update without peeking into db
	var nextToExpireIDs []ids.ID
	if !nextIDsIsEmpty {
		// calculating earliest next unlock time
		nextExpirationTime := *cs.proposalsNextExpirationTime
		for _, proposalDiff := range cs.modifiedProposals {
			if endtime := proposalDiff.Proposal.EndTime(); proposalDiff.added && endtime.Before(nextExpirationTime) {
				nextExpirationTime = endtime
			}
		}
		// adding current proposals
		if nextExpirationTime.Equal(*cs.proposalsNextExpirationTime) {
			for _, proposalID := range cs.proposalsNextToExpireIDs {
				if proposalDiff, ok := cs.modifiedProposals[proposalID]; !ok || !proposalDiff.removed {
					nextToExpireIDs = append(nextToExpireIDs, proposalID)
				}
			}
		}
		// adding new proposals
		needSort := false // proposalIDs from db are already sorted
		for proposalID, proposalDiff := range cs.modifiedProposals {
			if proposalDiff.added && proposalDiff.Proposal.EndTime().Equal(nextExpirationTime) {
				nextToExpireIDs = append(nextToExpireIDs, proposalID)
				needSort = true
			}
		}
		if needSort {
			utils.Sort(nextToExpireIDs)
		}
		cs.proposalsNextToExpireIDs = nextToExpireIDs
		cs.proposalsNextExpirationTime = &nextExpirationTime
	}

	// adding new proposals to db, deleting removed proposals from db
	for proposalID, proposalDiff := range cs.modifiedProposals {
		delete(cs.modifiedProposals, proposalID)
		if proposalDiff.removed {
			if err := cs.proposalsDB.Delete(proposalID[:]); err != nil {
				return err
			}
			if err := cs.proposalIDsByEndtimeDB.Delete(proposalToKey(proposalID[:], proposalDiff.Proposal)); err != nil {
				return err
			}
		} else {
			proposalBytes, err := dac.Codec.Marshal(blocks.Version, &proposalStateWrapper{ProposalState: proposalDiff.Proposal})
			if err != nil {
				return fmt.Errorf("failed to serialize deposit: %w", err)
			}
			if err := cs.proposalsDB.Put(proposalID[:], proposalBytes); err != nil {
				return err
			}
			if proposalDiff.added {
				if err := cs.proposalIDsByEndtimeDB.Put(proposalToKey(proposalID[:], proposalDiff.Proposal), nil); err != nil {
					return err
				}
			}
		}
	}

	// getting earliest proposals from db if proposalsNextToExpireIDs is empty
	if len(nextToExpireIDs) == 0 {
		nextToExpireIDs, nextExpirationTime, err := cs.getNextToExpireProposalIDsAndTimeFromDB(nil)
		switch {
		case err == database.ErrNotFound:
			cs.proposalsNextToExpireIDs = nil
			cs.proposalsNextExpirationTime = nil
		case err != nil:
			return err
		default:
			cs.proposalsNextToExpireIDs = nextToExpireIDs
			cs.proposalsNextExpirationTime = &nextExpirationTime
		}
	}

	// updating db of finished proposal ids

	proposalIDsToFinish, err := cs.GetProposalIDsToFinish()
	if err != nil {
		return err // Won't happen cause err is always nil here. Just in case if we'll change that
	}

	for proposalID, add := range cs.modifiedProposalIDsToFinish {
		if add {
			if err := cs.proposalIDsToFinishDB.Put(proposalID[:], nil); err != nil {
				return err
			}
		} else {
			if err := cs.proposalIDsToFinishDB.Delete(proposalID[:]); err != nil {
				return err
			}
		}
		delete(cs.modifiedProposalIDsToFinish, proposalID)
	}

	cs.proposalIDsToFinish = proposalIDsToFinish

	return nil
}

func (cs *caminoState) loadProposals() error {
	cs.proposalsNextToExpireIDs = nil
	cs.proposalsNextExpirationTime = nil
	proposalsNextToExpireIDs, proposalsNextExpirationTime, err := cs.getNextToExpireProposalIDsAndTimeFromDB(nil)
	if err == database.ErrNotFound {
		return nil
	} else if err != nil {
		return err
	}
	cs.proposalsNextToExpireIDs = proposalsNextToExpireIDs
	cs.proposalsNextExpirationTime = &proposalsNextExpirationTime

	// reading from db proposalIDs that are ready for execution
	proposalsToFinishIterator := cs.proposalIDsToFinishDB.NewIterator()
	defer proposalsToFinishIterator.Release()

	for proposalsToFinishIterator.Next() {
		proposalID, err := ids.ToID(proposalsToFinishIterator.Key())
		if err != nil {
			return err
		}
		cs.proposalIDsToFinish = append(cs.proposalIDsToFinish, proposalID)
	}

	if err := proposalsToFinishIterator.Error(); err != nil {
		return err
	}

	return nil
}

func (cs caminoState) getNextToExpireProposalIDsAndTimeFromDB(removedProposalIDs set.Set[ids.ID]) ([]ids.ID, time.Time, error) {
	proposalsIterator := cs.proposalIDsByEndtimeDB.NewIterator()
	defer proposalsIterator.Release()

	var nextProposalIDs []ids.ID
	nextProposalsEndTimestamp := uint64(math.MaxUint64)

	for proposalsIterator.Next() {
		proposalID, proposalEndtime, err := bytesToProposalIDAndEndtime(proposalsIterator.Key())
		if err != nil {
			return nil, time.Time{}, err
		}

		if removedProposalIDs.Contains(proposalID) {
			continue
		}

		// we expect values to be sorted by endtime in ascending order
		if proposalEndtime > nextProposalsEndTimestamp {
			break
		}
		if proposalEndtime < nextProposalsEndTimestamp {
			nextProposalsEndTimestamp = proposalEndtime
		}
		nextProposalIDs = append(nextProposalIDs, proposalID)
	}

	if err := proposalsIterator.Error(); err != nil {
		return nil, time.Time{}, err
	}

	if len(nextProposalIDs) == 0 {
		return nil, mockable.MaxTime, database.ErrNotFound
	}

	return nextProposalIDs, time.Unix(int64(nextProposalsEndTimestamp), 0), nil
}

// proposalID must be ids.ID 32 bytes
func proposalToKey(proposalID []byte, proposal dac.ProposalState) []byte {
	proposalSortKey := make([]byte, 8+32)
	binary.BigEndian.PutUint64(proposalSortKey, uint64(proposal.EndTime().Unix()))
	copy(proposalSortKey[8:], proposalID)
	return proposalSortKey
}

// proposalID must be ids.ID 32 bytes
func bytesToProposalIDAndEndtime(proposalSortKeyBytes []byte) (ids.ID, uint64, error) {
	proposalID, err := ids.ToID(proposalSortKeyBytes[8:])
	if err != nil {
		return ids.Empty, 0, err
	}
	return proposalID, binary.BigEndian.Uint64(proposalSortKeyBytes[:8]), nil
}

var _ ProposalsIterator = (*proposalsIterator)(nil)

type ProposalsIterator interface {
	Next() bool
	Value() (dac.ProposalState, error)
	Error() error
	Release()

	key() (ids.ID, error)
}

type proposalsIterator struct {
	caminoState *caminoState
	dbIterator  database.Iterator
	err         error
}

func (it *proposalsIterator) Next() bool {
	for it.dbIterator.Next() {
		proposalID, err := ids.ToID(it.dbIterator.Key())
		if err != nil { // should never happen
			it.err = err
			return false
		}
		if proposalDiff, ok := it.caminoState.modifiedProposals[proposalID]; !ok || !proposalDiff.removed {
			return true
		}
	}
	return false
}

func (it *proposalsIterator) Value() (dac.ProposalState, error) {
	proposalID, err := ids.ToID(it.dbIterator.Key())
	if err != nil { // should never happen
		return nil, err
	}

	if proposalDiff, ok := it.caminoState.modifiedProposals[proposalID]; ok {
		return proposalDiff.Proposal, nil
	}

	if proposal, ok := it.caminoState.proposalsCache.Get(proposalID); ok {
		return proposal, nil
	}

	proposal := &proposalStateWrapper{}
	if _, err := dac.Codec.Unmarshal(it.dbIterator.Value(), proposal); err != nil {
		return nil, err
	}

	return proposal.ProposalState, nil
}

func (it *proposalsIterator) Error() error {
	dbIteratorErr := it.dbIterator.Error()
	switch {
	case dbIteratorErr != nil && it.err != nil:
		return fmt.Errorf("%w, %s", it.err, dbIteratorErr)
	case dbIteratorErr == nil && it.err != nil:
		return it.err
	case dbIteratorErr != nil && it.err == nil:
		return dbIteratorErr
	}
	return nil
}

func (it *proposalsIterator) Release() {
	it.dbIterator.Release()
}

func (it *proposalsIterator) key() (ids.ID, error) {
	return ids.ToID(it.dbIterator.Key()) // err should never happen
}
