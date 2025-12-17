// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

func TestBloomSet_Refresh(t *testing.T) {
	type (
		op struct {
			add    *tx
			remove *tx
		}
		test struct {
			name                          string
			resetFalsePositiveProbability float64
			ops                           []op
			expectedInFilter              []tx
			expectedResetCount            float64
		}
	)
	tests := []test{
		{
			name:                          "no refresh",
			resetFalsePositiveProbability: 1, // maxCount = 9223372036854775807
			ops: []op{
				{add: &tx{0}},
				{add: &tx{1}},
				{add: &tx{2}},
			},
			expectedInFilter: []tx{
				{0},
				{1},
				{2},
			},
			expectedResetCount: 0,
		},
		{
			name:                          "no refresh - with removals",
			resetFalsePositiveProbability: 1, // maxCount = 9223372036854775807
			ops: []op{
				{add: &tx{0}},
				{add: &tx{1}},
				{add: &tx{2}},
				{remove: &tx{0}},
				{remove: &tx{1}},
				{remove: &tx{2}},
			},
			expectedInFilter: []tx{
				{0},
				{1},
				{2},
			},
			expectedResetCount: 0,
		},
		{
			name:                          "refresh",
			resetFalsePositiveProbability: 0.0000000000000001, // maxCount = 1
			ops: []op{
				{add: &tx{0}}, // no reset
				{remove: &tx{0}},
				{add: &tx{1}}, // reset
			},
			expectedInFilter: []tx{
				{1},
			},
			expectedResetCount: 1,
		},
		{
			name:                          "multiple refresh",
			resetFalsePositiveProbability: 0.0000000000000001, // maxCount = 1
			ops: []op{
				{add: &tx{0}}, // no reset
				{remove: &tx{0}},
				{add: &tx{1}}, // reset
				{remove: &tx{1}},
				{add: &tx{2}}, // reset
			},
			expectedInFilter: []tx{
				{2},
			},
			expectedResetCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				minTargetElements              = 1
				targetFalsePositiveProbability = 0.000001
			)
			var s setDouble
			m, err := bloom.NewMetrics("", prometheus.NewRegistry())
			require.NoError(t, err, "NewMetrics()")
			bs, err := NewBloomSet(
				&s,
				BloomSetConfig{
					Metrics:                        m,
					MinTargetElements:              minTargetElements,
					TargetFalsePositiveProbability: targetFalsePositiveProbability,
					ResetFalsePositiveProbability:  tt.resetFalsePositiveProbability,
				},
			)
			require.NoError(t, err, "NewBloomSet()")

			for _, op := range tt.ops {
				if op.add != nil {
					require.NoErrorf(t, bs.Add(*op.add), "%T.Add(...)", bs)
				}
				if op.remove != nil {
					s.txs.Remove(*op.remove)
				}
			}

			// Add one to expectedResetCount to account for the initial creation
			// of the bloom filter.
			require.Equal(t, tt.expectedResetCount+1, testutil.ToFloat64(m.ResetCount), "number of resets")
			b, h := bs.BloomFilter()
			for _, expected := range tt.expectedInFilter {
				require.Truef(t, bloom.Contains(b, expected[:], h[:]), "%T.Contains(%s)", b, expected.GossipID())
			}
		})
	}
}

// TestBloomSet_Concurrent tests that BloomSet ensures that the returned bloom
// filter is a super set of the items in the Set at the time it is called, even
// under concurrent resets, because resets of the filter are accompanied by
// refilling from the Set.
func TestBloomSet_Concurrent(t *testing.T) {
	var s setDouble
	bs, err := NewBloomSet(
		&s,
		BloomSetConfig{
			MinTargetElements:              1,
			TargetFalsePositiveProbability: 0.01,
			ResetFalsePositiveProbability:  0.0000000001, // Forces frequent resets
		},
	)
	require.NoError(t, err)

	var eg errgroup.Group
	for range 10 {
		eg.Go(func() error {
			for range 1000 {
				tx := tx(ids.GenerateTestID())
				if err := bs.Add(tx); err != nil {
					return err
				}

				bf, salt := bs.BloomFilter()
				if !bloom.Contains(bf, tx[:], salt[:]) {
					return fmt.Errorf("expected to find %s in bloom filter", tx)
				}

				s.Remove(tx)
			}
			return nil
		})
	}
	require.NoError(t, eg.Wait())
}
