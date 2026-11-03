// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

// TestAgreement checks that each node's accepted chain must be a prefix of
// the others', whichever node is ahead and whenever a lagging node catches
// up.
func TestAgreement(t *testing.T) {
	a, b, c, d := ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID(), ids.GenerateTestID()

	// Each step is one node's accepted chain as a check reads it.
	type step struct {
		node     int
		accepted []ids.ID
	}
	tests := []struct {
		name  string
		steps []step
		want  error
	}{
		{
			name:  "same_chain_at_different_heights",
			steps: []step{{0, []ids.ID{a, b, c}}, {1, []ids.ID{a}}, {2, nil}},
		},
		{
			name:  "lagging_node_catches_up",
			steps: []step{{0, []ids.ID{a, b}}, {1, []ids.ID{a}}, {1, []ids.ID{a, b, c}}, {0, []ids.ID{a, b, c, d}}},
		},
		{
			name:  "fork_at_the_tip",
			steps: []step{{0, []ids.ID{a, b}}, {1, []ids.ID{a, c}}},
			want:  errForked,
		},
		{
			name:  "lagging_node_catches_up_on_another_block",
			steps: []step{{0, []ids.ID{a, b, c}}, {1, []ids.ID{a}}, {1, []ids.ID{a, d}}},
			want:  errForked,
		},
		{
			name:  "node_ahead_on_another_block",
			steps: []step{{0, []ids.ID{a}}, {1, []ids.ID{a, b}}, {0, []ids.ID{a, c}}},
			want:  errForked,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ag agreement
			var err error
			for _, s := range tt.steps {
				if err = ag.add(s.node, s.accepted); err != nil {
					break
				}
			}
			require.ErrorIs(t, err, tt.want)
		})
	}
}
