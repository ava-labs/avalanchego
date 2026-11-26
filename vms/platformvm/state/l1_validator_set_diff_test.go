// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"bytes"
	"context"
	"maps"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"
)

func TestL1ValidatorSetDiffValue(t *testing.T) {
	tests := []struct {
		name  string
		entry *l1ValidatorSetEntry
	}{
		{
			name: "did_not_exist",
		},
		{
			name: "existed",
			entry: &l1ValidatorSetEntry{
				NodeID:    ids.GenerateTestNodeID(),
				Weight:    5,
				PublicKey: newL1ValidatorSetPublicKey(t),
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := unmarshalL1ValidatorSetDiffValue(marshalL1ValidatorSetDiffValue(test.entry))
			require.NoError(t, err)
			require.Equal(t, test.entry, got)
		})
	}

	invalidLengths := []struct {
		name   string
		length int
	}{
		{
			name:   "missing_weight",
			length: l1ValidatorSetDiffPublicKeyOffset - 1,
		},
		{
			name:   "missing_public_key",
			length: l1ValidatorSetDiffPublicKeyOffset,
		},
		{
			name:   "public_key_too_short",
			length: l1ValidatorSetDiffValueLength - 1,
		},
		{
			name:   "public_key_too_long",
			length: l1ValidatorSetDiffValueLength + 1,
		},
	}
	for _, test := range invalidLengths {
		t.Run(test.name, func(t *testing.T) {
			_, err := unmarshalL1ValidatorSetDiffValue(make([]byte, test.length))
			require.ErrorIs(t, err, errUnexpectedL1ValidatorSetDiffValueLength)
		})
	}
}

func TestGetL1ValidatorSet(t *testing.T) {
	var (
		subnetID   = ids.GenerateTestID()
		nodeID     = ids.GenerateTestNodeID()
		pk         = newL1ValidatorSetPublicKey(t)
		otherPK    = newL1ValidatorSetPublicKey(t)
		active     = newTestL1Validator(subnetID, nodeID, pk, 5, true)
		inactive   = newTestL1Validator(subnetID, nodeID, pk, 5, false)
		otherOwner = newTestL1Validator(subnetID, nodeID, otherPK, 7, true)
	)
	withWeight := func(v L1Validator, weight uint64) L1Validator {
		v.Weight = weight
		return v
	}
	withActive := func(v L1Validator, isActive bool) L1Validator {
		v.EndAccumulatedFee = 0
		if isActive {
			v.EndAccumulatedFee = 1
		}
		return v
	}

	tests := []struct {
		name string
		// blocks are accepted at heights 1, 2, ...; each one puts its L1
		// validators in order.
		blocks      [][]L1Validator
		wantNumRows int
	}{
		{
			name:        "register_active",
			blocks:      [][]L1Validator{{active}},
			wantNumRows: 1,
		},
		{
			name:        "register_inactive",
			blocks:      [][]L1Validator{{inactive}},
			wantNumRows: 1,
		},
		{
			name: "delete_while_active",
			blocks: [][]L1Validator{
				{active},
				{withWeight(active, 0)},
			},
			wantNumRows: 2,
		},
		{
			name: "delete_while_inactive",
			blocks: [][]L1Validator{
				{inactive},
				{withWeight(inactive, 0)},
			},
			wantNumRows: 2,
		},
		{
			name: "weight_change_while_inactive",
			blocks: [][]L1Validator{
				{inactive},
				{withWeight(inactive, 9)},
			},
			wantNumRows: 2,
		},
		{
			name: "deactivate_and_reactivate",
			blocks: [][]L1Validator{
				{active},
				{withActive(active, false)},
				{withActive(active, true)},
			},
			wantNumRows: 1,
		},
		{
			name: "register_and_delete_in_same_block",
			blocks: [][]L1Validator{
				{active, withWeight(active, 0)},
			},
			wantNumRows: 0,
		},
		{
			name: "weight_change_reverted_in_same_block",
			blocks: [][]L1Validator{
				{active},
				{withWeight(active, 9), withWeight(active, 5)},
			},
			wantNumRows: 1,
		},
		{
			name: "nodeID_reused_in_same_block",
			blocks: [][]L1Validator{
				{active},
				{withWeight(active, 0), otherOwner},
			},
			wantNumRows: 3,
		},
		{
			name: "nodeID_reused_in_later_block",
			blocks: [][]L1Validator{
				{active},
				{withWeight(active, 0)},
				{otherOwner},
			},
			wantNumRows: 3,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			s := newTestState(t, db)

			wantSets := []map[ids.NodeID]*validators.GetValidatorOutput{
				{}, // There are no L1 validators at genesis, so the subnet is not an L1.
			}
			l1Validators := make(map[ids.ID]L1Validator)
			for _, block := range test.blocks {
				acceptL1ValidatorSetBlocks(t, s, l1Validators, block)
				wantSets = append(wantSets, newWantL1ValidatorSet(l1Validators, subnetID))
			}

			require.Equal(test.wantNumRows, countL1ValidatorSetDiffs(t, s))
			requireBeforeAndAfterReload(t, db, s, func(s *State) {
				requireL1ValidatorSets(t, s, subnetID, wantSets)
			})
		})
	}
}

func TestGetL1ValidatorSetIndexedHeights(t *testing.T) {
	var (
		subnetID = ids.GenerateTestID()
		first    = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
		second   = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, false)
		third    = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
	)

	tests := []struct {
		name string
		// updateIndexedHeights simulates a version that doesn't write the L1
		// validator set diffs having run after height 2 was accepted.
		updateIndexedHeights func(t *testing.T, s *State)
		// wantLowerBound is the lowest height that can be queried after a block
		// is accepted at height 3 by the reloaded state.
		wantLowerBound uint64
	}{
		{
			name:                 "no_gap",
			updateIndexedHeights: func(*testing.T, *State) {},
			wantLowerBound:       0,
		},
		{
			name: "gap",
			updateIndexedHeights: func(t *testing.T, s *State) {
				// An older version accepted height 2 without indexing it.
				putL1ValidatorSetIndexedHeights(t, s, heightRange{LowerBound: 1, UpperBound: 1})
			},
			wantLowerBound: 2,
		},
		{
			name: "upper_bound_ahead",
			updateIndexedHeights: func(t *testing.T, s *State) {
				// The indexed heights claim a height that was never accepted.
				putL1ValidatorSetIndexedHeights(t, s, heightRange{LowerBound: 1, UpperBound: 3})
			},
			wantLowerBound: 2,
		},
		{
			name: "never_indexed",
			updateIndexedHeights: func(t *testing.T, s *State) {
				require.NoError(t, s.singletonDB.Delete(L1ValidatorSetHeightsIndexedKey))
			},
			wantLowerBound: 2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			s := newTestState(t, db)

			wantSets := []map[ids.NodeID]*validators.GetValidatorOutput{{}}
			l1Validators := make(map[ids.ID]L1Validator)
			for _, l1Validator := range []L1Validator{first, second} {
				acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{l1Validator})
				wantSets = append(wantSets, newWantL1ValidatorSet(l1Validators, subnetID))
			}

			test.updateIndexedHeights(t, s)
			require.NoError(s.baseDB.Commit())
			s = newTestState(t, db)

			// The current height can always be queried.
			got, err := s.GetL1ValidatorSet(t.Context(), 2, subnetID)
			require.NoError(err)
			require.Equal(wantSets[2], got)

			acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{third})
			wantSets = append(wantSets, newWantL1ValidatorSet(l1Validators, subnetID))

			for height, want := range wantSets {
				if uint64(height) < test.wantLowerBound {
					_, err := s.GetL1ValidatorSet(t.Context(), uint64(height), subnetID)
					require.ErrorIs(err, validators.ErrL1ValidatorSetNotIndexed)
					continue
				}
				requireL1ValidatorSet(t, s, uint64(height), subnetID, want)
			}
		})
	}

	t.Run("future_height", func(t *testing.T) {
		s := newTestState(t, memdb.New())
		_, err := s.GetL1ValidatorSet(t.Context(), 1, subnetID)
		require.ErrorIs(t, err, validators.ErrUnfinalizedHeight)
	})
}

func TestL1ValidatorSetDiffKeyInvalidLength(t *testing.T) {
	for _, length := range []int{0, l1ValidatorSetDiffKeyLength - 1, l1ValidatorSetDiffKeyLength + 1} {
		_, _, err := unmarshalL1ValidatorSetDiffKey(make([]byte, length))
		require.ErrorIs(t, err, errUnexpectedL1ValidatorSetDiffKeyLength, "length %d", length)
	}
}

func FuzzL1ValidatorSetDiffKey(f *testing.F) {
	f.Add([]byte{}, uint64(0), []byte{}, uint64(1))
	f.Add([]byte{0xff}, uint64(1<<63), []byte{0x01}, ^uint64(0))
	f.Fuzz(func(t *testing.T, subnetIDBytes []byte, height uint64, validationIDBytes []byte, otherHeight uint64) {
		require := require.New(t)

		var subnetID, validationID ids.ID
		copy(subnetID[:], subnetIDBytes)
		copy(validationID[:], validationIDBytes)

		key := marshalL1ValidatorSetDiffKey(subnetID, height, validationID)
		require.Len(key, l1ValidatorSetDiffKeyLength)
		require.True(bytes.HasPrefix(key, subnetID[:]))

		gotHeight, gotValidationID, err := unmarshalL1ValidatorSetDiffKey(key)
		require.NoError(err)
		require.Equal(height, gotHeight)
		require.Equal(validationID, gotValidationID)

		// Keys of the same subnet are ordered by decreasing height.
		otherKey := marshalL1ValidatorSetDiffKey(subnetID, otherHeight, validationID)
		switch {
		case height > otherHeight:
			require.Negative(bytes.Compare(key, otherKey))
		case height < otherHeight:
			require.Positive(bytes.Compare(key, otherKey))
		default:
			require.Equal(key, otherKey)
		}
	})
}

func FuzzL1ValidatorSetDiffValue(f *testing.F) {
	f.Add([]byte{})
	f.Add(make([]byte, l1ValidatorSetDiffPublicKeyOffset-1))
	f.Add(make([]byte, l1ValidatorSetDiffPublicKeyOffset))
	f.Add(make([]byte, l1ValidatorSetDiffValueLength))
	f.Add(make([]byte, l1ValidatorSetDiffValueLength+1))
	f.Fuzz(func(t *testing.T, value []byte) {
		require := require.New(t)

		entry, err := unmarshalL1ValidatorSetDiffValue(value)
		if len(value) != 0 && len(value) != l1ValidatorSetDiffValueLength {
			require.ErrorIs(err, errUnexpectedL1ValidatorSetDiffValueLength)
			return
		}
		require.NoError(err)
		require.Equal(len(value) == 0, entry == nil)

		// Every value that can be parsed must be written back unchanged.
		require.True(bytes.Equal(value, marshalL1ValidatorSetDiffValue(entry)))
	})
}

// TestGetL1ValidatorSetNotL1 checks that a subnet without L1 validators is
// reported as not being an L1, even if another subnet is one.
func TestGetL1ValidatorSetNotL1(t *testing.T) {
	require := require.New(t)

	var (
		subnetID      = ids.GenerateTestID()
		otherSubnetID = ids.GenerateTestID()
		l1Validators  = make(map[ids.ID]L1Validator)
	)
	db := memdb.New()
	s := newTestState(t, db)

	// Only the current height can be queried before any block is accepted.
	got, err := s.GetL1ValidatorSet(t.Context(), 0, subnetID)
	require.ErrorIs(err, validators.ErrNotL1)
	require.Nil(got)

	acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{
		newTestL1Validator(otherSubnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true),
	})

	requireBeforeAndAfterReload(t, db, s, func(s *State) {
		for height := range uint64(2) {
			got, err := s.GetL1ValidatorSet(t.Context(), height, subnetID)
			require.ErrorIs(err, validators.ErrNotL1)
			require.Nil(got)
		}
	})
}

// TestGetL1ValidatorSetSubnetIsolation checks that the diffs of a subnet are
// never applied to another subnet, even if their IDs only differ in the last
// byte.
func TestGetL1ValidatorSetSubnetIsolation(t *testing.T) {
	var (
		subnetID      = ids.ID{0x01}
		otherSubnetID = ids.ID{0x01, 31: 0x01}
		pk            = newL1ValidatorSetPublicKey(t)
		l1Validators  = make(map[ids.ID]L1Validator)
		vdr           = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), pk, 5, true)
		otherVdr      = newTestL1Validator(otherSubnetID, ids.GenerateTestNodeID(), pk, 7, false)
	)
	db := memdb.New()
	s := newTestState(t, db)

	wantSets := map[ids.ID][]map[ids.NodeID]*validators.GetValidatorOutput{
		subnetID:      {{}},
		otherSubnetID: {{}},
	}
	updatedOtherVdr := otherVdr
	updatedOtherVdr.Weight = 9
	deletedVdr := vdr
	deletedVdr.Weight = 0
	for _, block := range [][]L1Validator{
		{vdr},
		{otherVdr},
		{updatedOtherVdr, deletedVdr},
	} {
		acceptL1ValidatorSetBlocks(t, s, l1Validators, block)
		for subnetID := range wantSets {
			wantSets[subnetID] = append(wantSets[subnetID], newWantL1ValidatorSet(l1Validators, subnetID))
		}
	}

	requireBeforeAndAfterReload(t, db, s, func(s *State) {
		for subnetID, want := range wantSets {
			requireL1ValidatorSets(t, s, subnetID, want)
		}
	})
}

// TestGetL1ValidatorSetProposalAndOption checks the heights of a proposal block
// and its option, which are committed together.
//
// The diffs of both blocks are written at the height of the option, so the set
// at the height of the proposal block is the set at its parent's height, even
// if the proposal block changes the set. The weight and public key diffs
// behave the same way.
func TestGetL1ValidatorSetProposalAndOption(t *testing.T) {
	var (
		subnetID           = ids.GenerateTestID()
		parentVdr          = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
		proposalVdr        = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 6, false)
		optionVdr          = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 7, true)
		deletedParentVdr   = parentVdr
		reweightedVdr      = parentVdr
		deletedProposalVdr = proposalVdr
	)
	deletedParentVdr.Weight = 0
	reweightedVdr.Weight = 9
	deletedProposalVdr.Weight = 0

	tests := []struct {
		name string
		// gapBeforeProposal makes the proposal block the first indexed block
		// after a gap.
		gapBeforeProposal bool
		proposal          []L1Validator
		option            []L1Validator
		wantNumNewRows    int
	}{
		{
			name:           "both_blocks_change_the_set",
			proposal:       []L1Validator{proposalVdr},
			option:         []L1Validator{optionVdr},
			wantNumNewRows: 2,
		},
		{
			name:           "option_reverts_the_proposal",
			proposal:       []L1Validator{reweightedVdr},
			option:         []L1Validator{parentVdr},
			wantNumNewRows: 0,
		},
		{
			name:           "option_deletes_what_the_proposal_registered",
			proposal:       []L1Validator{proposalVdr, deletedParentVdr},
			option:         []L1Validator{deletedProposalVdr},
			wantNumNewRows: 1, // Only the deletion of parentVdr.
		},
		{
			name:              "first_indexed_block_after_a_gap",
			gapBeforeProposal: true,
			proposal:          []L1Validator{proposalVdr},
			option:            []L1Validator{optionVdr},
			wantNumNewRows:    2,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			db := memdb.New()
			s := newTestState(t, db)

			l1Validators := make(map[ids.ID]L1Validator)
			acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{parentVdr})
			parentSet := newWantL1ValidatorSet(l1Validators, subnetID)
			numRows := countL1ValidatorSetDiffs(t, s)

			if test.gapBeforeProposal {
				putL1ValidatorSetIndexedHeights(t, s, heightRange{LowerBound: 1, UpperBound: 0})
				require.NoError(s.baseDB.Commit())
				s = newTestState(t, db)
			}

			acceptL1ValidatorSetBlocks(t, s, l1Validators, test.proposal, test.option)
			wantSets := []map[ids.NodeID]*validators.GetValidatorOutput{
				{},
				parentSet,
				parentSet, // The proposal block's changes are indexed at the option's height.
				newWantL1ValidatorSet(l1Validators, subnetID),
			}

			require.Equal(test.wantNumNewRows, countL1ValidatorSetDiffs(t, s)-numRows)
			requireBeforeAndAfterReload(t, db, s, func(s *State) {
				for height, want := range wantSets {
					if test.gapBeforeProposal && height < 1 {
						// The proposal block at height 2 starts the indexed
						// heights, so height 1 is the lowest one that can be
						// queried.
						_, err := s.GetL1ValidatorSet(t.Context(), uint64(height), subnetID)
						require.ErrorIs(err, validators.ErrL1ValidatorSetNotIndexed)
						continue
					}
					requireL1ValidatorSet(t, s, uint64(height), subnetID, want)
				}
			})
		})
	}
}

// TestGetL1ValidatorSetCommitWithoutBlock checks that commits that don't
// accept a block, such as the ones done when a peer disconnects, don't change
// the index.
func TestGetL1ValidatorSetCommitWithoutBlock(t *testing.T) {
	require := require.New(t)

	var (
		subnetID     = ids.GenerateTestID()
		l1Validators = make(map[ids.ID]L1Validator)
	)
	db := memdb.New()
	s := newTestState(t, db)

	wantSets := []map[ids.NodeID]*validators.GetValidatorOutput{{}}
	acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{
		newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true),
	})
	wantSets = append(wantSets, newWantL1ValidatorSet(l1Validators, subnetID))
	numRows := countL1ValidatorSetDiffs(t, s)

	for range 2 {
		require.NoError(s.Commit())
		require.Equal(numRows, countL1ValidatorSetDiffs(t, s))
		requireL1ValidatorSets(t, s, subnetID, wantSets)

		// A restarted node hasn't set its height yet when it commits.
		s = newTestState(t, db)
	}
	requireL1ValidatorSets(t, s, subnetID, wantSets)
}

// TestGetL1ValidatorSetUncommittedBlock checks that a block that was accepted
// but not committed yet can't be queried, and doesn't change the result at
// the previous height.
func TestGetL1ValidatorSetUncommittedBlock(t *testing.T) {
	require := require.New(t)

	var (
		subnetID     = ids.GenerateTestID()
		l1Validators = make(map[ids.ID]L1Validator)
		vdr          = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
	)
	s := newTestState(t, memdb.New())

	acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{vdr})
	wantSets := []map[ids.NodeID]*validators.GetValidatorOutput{
		{},
		newWantL1ValidatorSet(l1Validators, subnetID),
	}

	deletedVdr := vdr
	deletedVdr.Weight = 0
	d := newDiffOn(t, s)
	require.NoError(d.PutL1Validator(deletedVdr))
	require.NoError(d.Apply(s))
	addL1ValidatorSetBlock(t, s)

	_, err := s.GetL1ValidatorSet(t.Context(), 2, subnetID)
	require.ErrorIs(err, validators.ErrUnfinalizedHeight)
	requireL1ValidatorSets(t, s, subnetID, wantSets)

	require.NoError(s.Commit())
	putTestL1Validator(l1Validators, deletedVdr)
	wantSets = append(wantSets, newWantL1ValidatorSet(l1Validators, subnetID))
	requireL1ValidatorSets(t, s, subnetID, wantSets)
}

func TestGetL1ValidatorSetCorruptedDiffs(t *testing.T) {
	var (
		subnetID = ids.GenerateTestID()
		vdr      = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
		// A point that isn't on the curve has the right length but can't be
		// deserialized.
		invalidPublicKey = make([]byte, 2*bls.PublicKeyLen)
	)
	invalidPublicKey[len(invalidPublicKey)-1] = 1
	require.Nil(t, bls.PublicKeyFromValidUncompressedBytes(invalidPublicKey))

	tests := []struct {
		name    string
		key     []byte
		value   []byte
		wantErr error
	}{
		{
			name:    "value_too_short",
			key:     marshalL1ValidatorSetDiffKey(subnetID, 2, ids.GenerateTestID()),
			value:   make([]byte, l1ValidatorSetDiffPublicKeyOffset-1),
			wantErr: errUnexpectedL1ValidatorSetDiffValueLength,
		},
		{
			name:    "public_key_too_short",
			key:     marshalL1ValidatorSetDiffKey(subnetID, 2, ids.GenerateTestID()),
			value:   make([]byte, l1ValidatorSetDiffValueLength-1),
			wantErr: errUnexpectedL1ValidatorSetDiffValueLength,
		},
		{
			name: "invalid_public_key",
			key:  marshalL1ValidatorSetDiffKey(subnetID, 2, ids.GenerateTestID()),
			value: marshalL1ValidatorSetDiffValue(&l1ValidatorSetEntry{
				NodeID:    ids.GenerateTestNodeID(),
				Weight:    1,
				PublicKey: invalidPublicKey,
			}),
			wantErr: errL1ValidatorSetInvalidPublicKey,
		},
		{
			name:    "key_too_long",
			key:     append(marshalL1ValidatorSetDiffKey(subnetID, 2, ids.GenerateTestID()), 0x00),
			value:   []byte{},
			wantErr: errUnexpectedL1ValidatorSetDiffKeyLength,
		},
		{
			// Restores another validator with the NodeID of vdr, which still
			// exists at height 1.
			name: "duplicate_nodeID",
			key:  marshalL1ValidatorSetDiffKey(subnetID, 2, ids.GenerateTestID()),
			value: marshalL1ValidatorSetDiffValue(&l1ValidatorSetEntry{
				NodeID:    vdr.NodeID,
				Weight:    1,
				PublicKey: vdr.PublicKey,
			}),
			wantErr: errL1ValidatorSetDuplicateNodeID,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			s := newTestState(t, memdb.New())
			l1Validators := make(map[ids.ID]L1Validator)
			acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{vdr})
			acceptL1ValidatorSetBlocks(t, s, l1Validators, []L1Validator{
				newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 3, false),
			})
			require.NoError(s.l1ValidatorSetDiffsDB.Put(test.key, test.value))

			_, err := s.GetL1ValidatorSet(t.Context(), 1, subnetID)
			require.ErrorIs(err, test.wantErr)
		})
	}
}

func TestGetL1ValidatorSetContextCanceled(t *testing.T) {
	var (
		subnetID = ids.GenerateTestID()
		vdr      = newTestL1Validator(subnetID, ids.GenerateTestNodeID(), newL1ValidatorSetPublicKey(t), 5, true)
	)
	s := newTestState(t, memdb.New())
	acceptL1ValidatorSetBlocks(t, s, make(map[ids.ID]L1Validator), []L1Validator{vdr})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := s.GetL1ValidatorSet(ctx, 0, subnetID)
	require.ErrorIs(t, err, context.Canceled)
}

// FuzzGetL1ValidatorSet checks the L1 validator set at every height against a
// model, over random histories of blocks, proposal blocks accepted with their
// options, restarts and gaps in the indexed heights.
func FuzzGetL1ValidatorSet(f *testing.F) {
	for seed := range int64(20) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, seed int64) {
		const (
			numSteps          = 25
			maxUpdatesInBlock = 4
			numNodeIDs        = 4
			numPublicKeys     = 3
			maxWeight         = 10
		)
		require := require.New(t)
		rng := rand.New(rand.NewSource(seed)) //#nosec G404

		var (
			subnetIDs  = []ids.ID{ids.GenerateTestID(), ids.GenerateTestID()}
			nodeIDs    = make([]ids.NodeID, numNodeIDs)
			publicKeys = make([][]byte, numPublicKeys)
		)
		for i := range nodeIDs {
			nodeIDs[i] = ids.GenerateTestNodeID()
		}
		for i := range publicKeys {
			publicKeys[i] = newL1ValidatorSetPublicKey(t)
		}
		newBlock := func(l1Validators map[ids.ID]L1Validator) []L1Validator {
			// Generate the updates on a copy, so that they are valid on top of
			// each other without changing the model before the block is
			// accepted.
			next := maps.Clone(l1Validators)
			var block []L1Validator
			for range rng.Intn(maxUpdatesInBlock + 1) {
				l1Validator, ok := newRandomL1ValidatorUpdate(rng, next, subnetIDs, nodeIDs, publicKeys, maxWeight)
				if !ok {
					continue
				}
				block = append(block, l1Validator)
				putTestL1Validator(next, l1Validator)
			}
			return block
		}

		db := memdb.New()
		s := newTestState(t, db)

		var (
			l1Validators = make(map[ids.ID]L1Validator)
			wantSets     = make(map[ids.ID][]map[ids.NodeID]*validators.GetValidatorOutput)
			// minIndexedHeight is the lowest height that can be queried.
			minIndexedHeight uint64
		)
		appendWantSets := func() {
			for _, subnetID := range subnetIDs {
				wantSets[subnetID] = append(wantSets[subnetID], newWantL1ValidatorSet(l1Validators, subnetID))
			}
		}
		appendWantSets()

		requireModel := func(s *State) {
			t.Helper()
			for _, subnetID := range subnetIDs {
				for height, want := range wantSets[subnetID] {
					if uint64(height) < minIndexedHeight {
						_, err := s.GetL1ValidatorSet(t.Context(), uint64(height), subnetID)
						require.ErrorIs(err, validators.ErrL1ValidatorSetNotIndexed, "height %d", height)
						continue
					}
					requireL1ValidatorSet(t, s, uint64(height), subnetID, want)
				}
				_, err := s.GetL1ValidatorSet(t.Context(), uint64(len(wantSets[subnetID])), subnetID)
				require.ErrorIs(err, validators.ErrUnfinalizedHeight)
			}
		}

		for range numSteps {
			switch rng.Intn(10) {
			case 0:
				s = newTestState(t, db)
			case 1:
				// An older version accepts blocks without indexing them, so
				// the indexed heights don't end at the last accepted height.
				// After the restart, only the last accepted height and the
				// heights accepted from then on can be queried.
				height := uint64(len(wantSets[subnetIDs[0]]) - 1)
				upperBound := height + 1 + uint64(rng.Intn(3))
				if height > 0 && rng.Intn(2) == 0 {
					upperBound = uint64(rng.Int63n(int64(height)))
				}
				putL1ValidatorSetIndexedHeights(t, s, heightRange{
					LowerBound: 0,
					UpperBound: upperBound,
				})
				require.NoError(s.baseDB.Commit())
				s = newTestState(t, db)
				minIndexedHeight = height
			case 2:
				proposal := newBlock(l1Validators)
				parentSets := make(map[ids.ID]map[ids.NodeID]*validators.GetValidatorOutput)
				for _, subnetID := range subnetIDs {
					parentSets[subnetID] = wantSets[subnetID][len(wantSets[subnetID])-1]
				}
				next := maps.Clone(l1Validators)
				for _, l1Validator := range proposal {
					putTestL1Validator(next, l1Validator)
				}
				option := newBlock(next)
				acceptL1ValidatorSetBlocks(t, s, l1Validators, proposal, option)
				for _, subnetID := range subnetIDs {
					wantSets[subnetID] = append(wantSets[subnetID], parentSets[subnetID])
				}
				appendWantSets()
			default:
				acceptL1ValidatorSetBlocks(t, s, l1Validators, newBlock(l1Validators))
				appendWantSets()
			}
			requireModel(s)
		}
		requireBeforeAndAfterReload(t, db, s, requireModel)
	})
}

func newL1ValidatorSetPublicKey(t *testing.T) []byte {
	sk, err := localsigner.New()
	require.NoError(t, err)
	return bls.PublicKeyToUncompressedBytes(sk.PublicKey())
}

func newTestL1Validator(subnetID ids.ID, nodeID ids.NodeID, pk []byte, weight uint64, isActive bool) L1Validator {
	l1Validator := L1Validator{
		ValidationID:          ids.GenerateTestID(),
		SubnetID:              subnetID,
		NodeID:                nodeID,
		PublicKey:             pk,
		RemainingBalanceOwner: []byte{},
		DeactivationOwner:     []byte{},
		Weight:                weight,
	}
	if isActive {
		l1Validator.EndAccumulatedFee = 1
	}
	return l1Validator
}

// newRandomL1ValidatorUpdate returns a random L1 validator update that is valid
// on top of l1Validators. It returns false if no update was generated.
func newRandomL1ValidatorUpdate(
	rng *rand.Rand,
	l1Validators map[ids.ID]L1Validator,
	subnetIDs []ids.ID,
	nodeIDs []ids.NodeID,
	publicKeys [][]byte,
	maxWeight int,
) (L1Validator, bool) {
	randomWeight := func() uint64 {
		return uint64(rng.Intn(maxWeight) + 1)
	}

	// Iterate the existing validators in a deterministic order.
	var existing []L1Validator
	for _, subnetID := range subnetIDs {
		for _, nodeID := range nodeIDs {
			for _, l1Validator := range l1Validators {
				if l1Validator.SubnetID == subnetID && l1Validator.NodeID == nodeID {
					existing = append(existing, l1Validator)
				}
			}
		}
	}

	const (
		register = iota
		changeWeight
		remove
		toggleActive
		numUpdates
	)
	update := rng.Intn(numUpdates)
	if update == register || len(existing) == 0 {
		subnetID := subnetIDs[rng.Intn(len(subnetIDs))]
		nodeID := nodeIDs[rng.Intn(len(nodeIDs))]
		for _, l1Validator := range existing {
			if l1Validator.SubnetID == subnetID && l1Validator.NodeID == nodeID {
				return L1Validator{}, false
			}
		}
		pk := publicKeys[rng.Intn(len(publicKeys))]
		return newTestL1Validator(subnetID, nodeID, pk, randomWeight(), rng.Intn(2) == 0), true
	}

	l1Validator := existing[rng.Intn(len(existing))]
	switch update {
	case changeWeight:
		l1Validator.Weight = randomWeight()
	case remove:
		l1Validator.Weight = 0
	case toggleActive:
		if l1Validator.EndAccumulatedFee == 0 {
			l1Validator.EndAccumulatedFee = 1
		} else {
			l1Validator.EndAccumulatedFee = 0
		}
	}
	return l1Validator, true
}

func putTestL1Validator(l1Validators map[ids.ID]L1Validator, l1Validator L1Validator) {
	if l1Validator.isDeleted() {
		delete(l1Validators, l1Validator.ValidationID)
		return
	}
	l1Validators[l1Validator.ValidationID] = l1Validator
}

func newWantL1ValidatorSet(l1Validators map[ids.ID]L1Validator, subnetID ids.ID) map[ids.NodeID]*validators.GetValidatorOutput {
	validatorSet := make(map[ids.NodeID]*validators.GetValidatorOutput)
	for _, l1Validator := range l1Validators {
		if l1Validator.SubnetID != subnetID {
			continue
		}
		validatorSet[l1Validator.NodeID] = &validators.GetValidatorOutput{
			NodeID:    l1Validator.NodeID,
			PublicKey: bls.PublicKeyFromValidUncompressedBytes(l1Validator.PublicKey),
			Weight:    l1Validator.Weight,
		}
	}
	return validatorSet
}

// acceptL1ValidatorSetBlocks accepts a block for each element of blocks, which
// puts its L1 validators in order, and commits all of them at once. This is how
// a proposal block is accepted together with its option.
func acceptL1ValidatorSetBlocks(t *testing.T, s *State, l1Validators map[ids.ID]L1Validator, blocks ...[]L1Validator) {
	t.Helper()

	for _, block := range blocks {
		d := newDiffOn(t, s)
		for _, l1Validator := range block {
			require.NoError(t, d.PutL1Validator(l1Validator))
			putTestL1Validator(l1Validators, l1Validator)
		}
		require.NoError(t, d.Apply(s))
		addL1ValidatorSetBlock(t, s)
	}
	require.NoError(t, s.Commit())
}

// addL1ValidatorSetBlock marks a new block as accepted on top of the last
// accepted block, without committing it.
func addL1ValidatorSetBlock(t *testing.T, s *State) {
	t.Helper()

	parent, err := s.GetStatelessBlock(s.GetLastAccepted())
	require.NoError(t, err)
	blk, err := platform.NewBanffStandardBlock(s.GetTimestamp(), parent.ID(), parent.Height()+1, nil)
	require.NoError(t, err)

	s.AddStatelessBlock(blk)
	s.SetLastAccepted(blk.ID())
	s.SetHeight(blk.Height())
}

// requireL1ValidatorSets requires the L1 validator set of subnetID at every
// height to be wantSets[height], where the last height is the current one.
func requireL1ValidatorSets(
	t *testing.T,
	s *State,
	subnetID ids.ID,
	wantSets []map[ids.NodeID]*validators.GetValidatorOutput,
) {
	t.Helper()

	for height, want := range wantSets {
		requireL1ValidatorSet(t, s, uint64(height), subnetID, want)
	}
}

// requireL1ValidatorSet requires the L1 validator set of subnetID at height to
// be want. A subnet without L1 validators is not an L1, so an empty want
// requires validators.ErrNotL1.
func requireL1ValidatorSet(
	t *testing.T,
	s *State,
	height uint64,
	subnetID ids.ID,
	want map[ids.NodeID]*validators.GetValidatorOutput,
) {
	t.Helper()

	got, err := s.GetL1ValidatorSet(t.Context(), height, subnetID)
	if len(want) == 0 {
		require.ErrorIs(t, err, validators.ErrNotL1, "height %d", height)
		return
	}
	require.NoError(t, err, "height %d", height)
	require.Equal(t, want, got, "height %d", height)
}

func countL1ValidatorSetDiffs(t *testing.T, s *State) int {
	t.Helper()

	it := s.l1ValidatorSetDiffsDB.NewIterator()
	defer it.Release()

	var numRows int
	for it.Next() {
		numRows++
	}
	require.NoError(t, it.Error())
	return numRows
}

func putL1ValidatorSetIndexedHeights(t *testing.T, s *State, indexedHeights heightRange) {
	t.Helper()

	indexedHeightsBytes, err := platform.GenesisCodec.Marshal(platform.CodecVersion, &indexedHeights)
	require.NoError(t, err)
	require.NoError(t, s.singletonDB.Put(L1ValidatorSetHeightsIndexedKey, indexedHeightsBytes))
}
