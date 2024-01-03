// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/btree"
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

func TestMaskedIterator(t *testing.T) {
	require := require.New(t)
	stakers := []*Staker{
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(0, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(1, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(2, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			NextTime: time.Unix(3, 0),
		},
		{
			TxID:     ids.GenerateTestID(),
			Weight:   40, // just to simplify debugging
			NextTime: time.Unix(40, 0),
		},
	}
	maskedStakers := map[ids.ID]*Staker{
		stakers[1].TxID: stakers[1],
	}

	updatedStaker := *stakers[0]
	updatedStaker.Weight = 50
	updatedStaker.NextTime = time.Unix(50, 0)
	updatedStakers := map[ids.ID]*Staker{
		updatedStaker.TxID: &updatedStaker,
	}

	it := NewMaskedIterator(
		NewSliceIterator(stakers...),
		maskedStakers,
		updatedStakers,
	)

	require.True(it.Next())
	require.Equal(stakers[2], it.Value())

	require.True(it.Next())
	require.Equal(stakers[3], it.Value())

	require.True(it.Next())
	require.Equal(stakers[4], it.Value())

	require.True(it.Next())
	require.Equal(&updatedStaker, it.Value())

	require.False(it.Next())
	it.Release()
	require.False(it.Next())
}

func TestMaskIteratorProperties(t *testing.T) {
	properties := gopter.NewProperties(nil)

	properties.Property("Mask iterator output must be sorted", prop.ForAll(
		func(nonInitParentStakerTxs []*txs.Tx, indexes []int, counts []int) string {
			deletedCount := counts[0]
			updatedCount := counts[1]
			deletedIndexes := indexes[0:deletedCount]
			updatedIndexes := indexes[deletedCount : deletedCount+updatedCount]

			parentStakers := make([]*Staker, 0, len(nonInitParentStakerTxs))
			for _, nonInitParentStakerTx := range nonInitParentStakerTxs {
				signedTx, err := txs.NewSigned(nonInitParentStakerTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				stakerTx := signedTx.Unsigned.(txs.StakerTx)
				startTime := signedTx.Unsigned.(txs.ScheduledStaker).StartTime()
				staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, startTime, uint64(100))
				if err != nil {
					return err.Error()
				}
				parentStakers = append(parentStakers, staker)
			}

			_, _, maskedIt := buildMaskedIterator(parentStakers, deletedIndexes, updatedIndexes)

			res := make([]*Staker, 0, len(parentStakers))
			for maskedIt.Next() {
				cpy := *maskedIt.Value()
				res = append(res, &cpy)
			}

			for idx := 1; idx < len(res); idx++ {
				if !res[idx-1].Less(res[idx]) {
					return "out of order stakers"
				}
			}

			return ""
		},
		maskedIteratorTestGenerator()...,
	))

	properties.Property("Masked stakers must not be in the output", prop.ForAll(
		func(nonInitParentStakerTxs []*txs.Tx, indexes []int, counts []int) string {
			deletedCount := counts[0]
			updatedCount := counts[1]
			deletedIndexes := indexes[0:deletedCount]
			updatedIndexes := indexes[deletedCount : deletedCount+updatedCount]

			parentStakers := make([]*Staker, 0, len(nonInitParentStakerTxs))
			for _, nonInitParentStakerTx := range nonInitParentStakerTxs {
				signedTx, err := txs.NewSigned(nonInitParentStakerTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				stakerTx := signedTx.Unsigned.(txs.StakerTx)
				startTime := signedTx.Unsigned.(txs.ScheduledStaker).StartTime()
				staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, startTime, uint64(100))
				if err != nil {
					return err.Error()
				}
				parentStakers = append(parentStakers, staker)
			}

			deleted, _, maskedIt := buildMaskedIterator(parentStakers, deletedIndexes, updatedIndexes)

			res := set.NewSet[ids.ID](0)
			for maskedIt.Next() {
				res.Add(maskedIt.Value().TxID)
			}

			for id := range deleted {
				if res.Contains(id) {
					return "deleted stakers returned when it should not have"
				}
			}

			return ""
		},
		maskedIteratorTestGenerator()...,
	))

	properties.Property("Updated stakers must be returned instead of their parent version", prop.ForAll(
		func(nonInitParentStakerTxs []*txs.Tx, indexes []int, counts []int) string {
			deletedCount := counts[0]
			updatedCount := counts[1]
			deletedIndexes := indexes[0:deletedCount]
			updatedIndexes := indexes[deletedCount : deletedCount+updatedCount]

			parentStakers := make([]*Staker, 0, len(nonInitParentStakerTxs))
			for _, nonInitParentStakerTx := range nonInitParentStakerTxs {
				signedTx, err := txs.NewSigned(nonInitParentStakerTx.Unsigned, txs.Codec, nil)
				if err != nil {
					panic(fmt.Errorf("failed signing tx in tx generator, %w", err))
				}

				stakerTx := signedTx.Unsigned.(txs.StakerTx)
				startTime := signedTx.Unsigned.(txs.ScheduledStaker).StartTime()
				staker, err := NewCurrentStaker(signedTx.ID(), stakerTx, startTime, uint64(100))
				if err != nil {
					return err.Error()
				}
				parentStakers = append(parentStakers, staker)
			}

			_, updated, maskedIt := buildMaskedIterator(parentStakers, deletedIndexes, updatedIndexes)

			res := make(map[ids.ID]*Staker)
			for maskedIt.Next() {
				staker := maskedIt.Value()
				res[staker.TxID] = staker
			}

			for id, up := range updated {
				ret, found := res[id]
				if !found {
					return "updated staker not found"
				}

				if !reflect.DeepEqual(ret, up) {
					return "updated staker different from expected"
				}
			}

			return ""
		},
		maskedIteratorTestGenerator()...,
	))

	properties.TestingRun(t)
}

func indexPermutationGenerator(sliceLen int) gopter.Gen {
	return gen.SliceOfN(sliceLen, gen.Int()).FlatMap(
		func(v interface{}) gopter.Gen {
			randomIndexes := v.([]int)

			sorted := make([]int, len(randomIndexes))
			copy(sorted, randomIndexes)
			sort.Ints(sorted)

			res := make([]int, 0, len(randomIndexes))

			for _, rnd := range randomIndexes {
				idx := 0
				for ; sorted[idx] != rnd; idx++ {
				}

				res = append(res, idx)
			}
			return gen.Const(res)
		},
		reflect.TypeOf([]int{}),
	)
}

func maskedIteratorTestGenerator() []gopter.Gen {
	parentStakersCount := 10
	ctx := snowtest.Context(&testing.T{}, snowtest.PChainID)

	return []gopter.Gen{
		gen.SliceOfN(parentStakersCount, addValidatorTxGenerator(ctx, nil, math.MaxUint64)),
		indexPermutationGenerator(parentStakersCount),
		gen.SliceOfN(2, gen.IntRange(0, parentStakersCount)).SuchThat(func(v interface{}) bool {
			nums := v.([]int)
			deletedCount := nums[0]
			updatedCount := nums[1]
			return deletedCount+updatedCount <= parentStakersCount
		}),
	}
}

func buildMaskedIterator(parentStakers []*Staker, deletedIndexes []int, updatedIndexes []int) (
	map[ids.ID]*Staker, // deletedStakers
	map[ids.ID]*Staker, // updatedStakers
	StakerIterator,
) {
	parentTree := btree.NewG(defaultTreeDegree, (*Staker).Less)
	for idx := range parentStakers {
		s := parentStakers[idx]
		parentTree.ReplaceOrInsert(s)
	}
	parentIt := NewTreeIterator(parentTree)

	deletedStakers := make(map[ids.ID]*Staker)
	for _, idx := range deletedIndexes {
		s := parentStakers[idx]
		deletedStakers[s.TxID] = s
	}

	updatedStakers := make(map[ids.ID]*Staker)
	for _, idx := range updatedIndexes {
		s := parentStakers[idx]
		ShiftStakerAheadInPlace(s, s.EndTime)
		updatedStakers[s.TxID] = s
	}

	return deletedStakers, updatedStakers, NewMaskedIterator(parentIt, deletedStakers, updatedStakers)
}
