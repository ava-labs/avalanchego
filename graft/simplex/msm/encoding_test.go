// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"testing"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

func TestSimplexEpochInfoIsZero(t *testing.T) {
	require.True(t, (&SimplexEpochInfo{}).IsZero())
	require.False(t, (&SimplexEpochInfo{PChainReferenceHeight: 1}).IsZero())
}

func TestSimplexEpochInfoEqual(t *testing.T) {
	hash1 := common.Digest{1, 2, 3}
	hash2 := common.Digest{4, 5, 6}

	tests := []struct {
		name     string
		a        *SimplexEpochInfo
		b        *SimplexEpochInfo
		expected bool
	}{
		{
			name:     "first nil second nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "first non-nil second nil",
			a:        &SimplexEpochInfo{},
			b:        nil,
			expected: false,
		},
		{
			name:     "first nil second non-nil (via nil receiver)",
			a:        nil,
			b:        &SimplexEpochInfo{},
			expected: false,
		},
		{
			name:     "both zero",
			a:        &SimplexEpochInfo{},
			b:        &SimplexEpochInfo{},
			expected: true,
		},
		{
			name: "equal with all fields",
			a: &SimplexEpochInfo{
				PChainReferenceHeight:     10,
				PrevSealingBlockHash:      hash1,
				NextPChainReferenceHeight: 20,
				PrevVMBlockSeq:            5,
				SealingBlockSeq:           15,
			},
			b: &SimplexEpochInfo{
				PChainReferenceHeight:     10,
				PrevSealingBlockHash:      hash1,
				NextPChainReferenceHeight: 20,
				PrevVMBlockSeq:            5,
				SealingBlockSeq:           15,
			},
			expected: true,
		},
		{
			name:     "different PChainReferenceHeight",
			a:        &SimplexEpochInfo{PChainReferenceHeight: 1},
			b:        &SimplexEpochInfo{PChainReferenceHeight: 2},
			expected: false,
		},
		{
			name:     "different PrevSealingBlockHash",
			a:        &SimplexEpochInfo{PrevSealingBlockHash: hash1},
			b:        &SimplexEpochInfo{PrevSealingBlockHash: hash2},
			expected: false,
		},
		{
			name:     "different NextPChainReferenceHeight",
			a:        &SimplexEpochInfo{NextPChainReferenceHeight: 1},
			b:        &SimplexEpochInfo{NextPChainReferenceHeight: 2},
			expected: false,
		},
		{
			name:     "different PrevVMBlockSeq",
			a:        &SimplexEpochInfo{PrevVMBlockSeq: 1},
			b:        &SimplexEpochInfo{PrevVMBlockSeq: 2},
			expected: false,
		},
		{
			name:     "different SealingBlockSeq",
			a:        &SimplexEpochInfo{SealingBlockSeq: 1},
			b:        &SimplexEpochInfo{SealingBlockSeq: 2},
			expected: false,
		},
		{
			name: "with BlockValidationDescriptor equal",
			a: &SimplexEpochInfo{
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{
						Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
					},
				},
			},
			b: &SimplexEpochInfo{
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{
						Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
					},
				},
			},
			expected: true,
		},
		{
			name: "with BlockValidationDescriptor different",
			a: &SimplexEpochInfo{
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{
						Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
					},
				},
			},
			b: &SimplexEpochInfo{
				BlockValidationDescriptor: &BlockValidationDescriptor{
					AggregatedMembership: AggregatedMembership{
						Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{2}, Weight: 20}},
					},
				},
			},
			expected: false,
		},
		{
			name: "with NextEpochApprovals equal",
			a: &SimplexEpochInfo{
				NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1, 2}, Signature: []byte{3, 4}},
			},
			b: &SimplexEpochInfo{
				NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1, 2}, Signature: []byte{3, 4}},
			},
			expected: true,
		},
		{
			name: "with NextEpochApprovals different",
			a: &SimplexEpochInfo{
				NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{1, 2}, Signature: []byte{3, 4}},
			},
			b: &SimplexEpochInfo{
				NextEpochApprovals: &NextEpochApprovals{NodeIDs: []byte{5, 6}, Signature: []byte{7, 8}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equal(tt.b))
		})
	}
}

func TestNodeBLSMappingEquals(t *testing.T) {
	tests := []struct {
		name     string
		a        NodeBLSMapping
		b        NodeBLSMapping
		expected bool
	}{
		{
			name:     "both zero",
			expected: true,
		},
		{
			name:     "equal with values",
			a:        NodeBLSMapping{NodeID: avalanchego.NodeID{1, 2, 3}, BLSKey: []byte{4, 5}, Weight: 100},
			b:        NodeBLSMapping{NodeID: avalanchego.NodeID{1, 2, 3}, BLSKey: []byte{4, 5}, Weight: 100},
			expected: true,
		},
		{
			name:     "different NodeID",
			a:        NodeBLSMapping{NodeID: avalanchego.NodeID{1}},
			b:        NodeBLSMapping{NodeID: avalanchego.NodeID{2}},
			expected: false,
		},
		{
			name:     "different BLSKey",
			a:        NodeBLSMapping{BLSKey: []byte{1}},
			b:        NodeBLSMapping{BLSKey: []byte{2}},
			expected: false,
		},
		{
			name:     "different Weight",
			a:        NodeBLSMapping{Weight: 1},
			b:        NodeBLSMapping{Weight: 2},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equals(&tt.b))
		})
	}
}

func TestBlockValidationDescriptorEquals(t *testing.T) {
	tests := []struct {
		name     string
		a        *BlockValidationDescriptor
		b        *BlockValidationDescriptor
		expected bool
	}{
		{
			name:     "both nil",
			expected: true,
		},
		{
			name:     "first nil second non-nil",
			b:        &BlockValidationDescriptor{},
			expected: false,
		},
		{
			name:     "first non-nil second nil",
			a:        &BlockValidationDescriptor{},
			expected: false,
		},
		{
			name: "equal members",
			a: &BlockValidationDescriptor{
				AggregatedMembership: AggregatedMembership{
					Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
				},
			},
			b: &BlockValidationDescriptor{
				AggregatedMembership: AggregatedMembership{
					Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
				},
			},
			expected: true,
		},
		{
			name: "different members",
			a: &BlockValidationDescriptor{
				AggregatedMembership: AggregatedMembership{
					Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
				},
			},
			b: &BlockValidationDescriptor{
				AggregatedMembership: AggregatedMembership{
					Members: []NodeBLSMapping{{NodeID: avalanchego.NodeID{2}, Weight: 20}},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equals(tt.b))
		})
	}
}

func TestAggregatedMembershipEquals(t *testing.T) {
	tests := []struct {
		name     string
		members  []NodeBLSMapping
		other    []NodeBLSMapping
		expected bool
	}{
		{
			name:     "both empty",
			expected: true,
		},
		{
			name:     "different lengths",
			members:  []NodeBLSMapping{{Weight: 1}},
			expected: false,
		},
		{
			name:     "equal",
			members:  []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, BLSKey: []byte{2}, Weight: 3}},
			other:    []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}, BLSKey: []byte{2}, Weight: 3}},
			expected: true,
		},
		{
			name:     "different",
			members:  []NodeBLSMapping{{NodeID: avalanchego.NodeID{1}}},
			other:    []NodeBLSMapping{{NodeID: avalanchego.NodeID{2}}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			am := &AggregatedMembership{Members: tt.members}
			require.Equal(t, tt.expected, am.Equals(tt.other))
		})
	}
}

func TestNextEpochApprovalsEquals(t *testing.T) {
	tests := []struct {
		name     string
		a        *NextEpochApprovals
		b        *NextEpochApprovals
		expected bool
	}{
		{
			name:     "both nil",
			expected: true,
		},
		{
			name:     "first nil second non-nil",
			b:        &NextEpochApprovals{},
			expected: false,
		},
		{
			name:     "first non-nil second nil",
			a:        &NextEpochApprovals{},
			expected: false,
		},
		{
			name:     "equal",
			a:        &NextEpochApprovals{NodeIDs: []byte{1, 2}, Signature: []byte{3, 4}},
			b:        &NextEpochApprovals{NodeIDs: []byte{1, 2}, Signature: []byte{3, 4}},
			expected: true,
		},
		{
			name:     "different NodeIDs",
			a:        &NextEpochApprovals{NodeIDs: []byte{1}},
			b:        &NextEpochApprovals{NodeIDs: []byte{2}},
			expected: false,
		},
		{
			name:     "different Signature",
			a:        &NextEpochApprovals{Signature: []byte{1}},
			b:        &NextEpochApprovals{Signature: []byte{2}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equals(tt.b))
		})
	}
}

func TestNodeBLSMappingsCompare(t *testing.T) {
	tests := []struct {
		name     string
		a        NodeBLSMappings
		b        NodeBLSMappings
		expected bool
	}{
		{
			name:     "both nil",
			expected: true,
		},
		{
			name:     "different lengths",
			a:        NodeBLSMappings{{Weight: 1}},
			expected: false,
		},
		{
			name:     "equal same order",
			a:        NodeBLSMappings{{NodeID: avalanchego.NodeID{1}, Weight: 10}, {NodeID: avalanchego.NodeID{2}, Weight: 20}},
			b:        NodeBLSMappings{{NodeID: avalanchego.NodeID{1}, Weight: 10}, {NodeID: avalanchego.NodeID{2}, Weight: 20}},
			expected: true,
		},
		{
			name:     "equal different order",
			a:        NodeBLSMappings{{NodeID: avalanchego.NodeID{2}, Weight: 20}, {NodeID: avalanchego.NodeID{1}, Weight: 10}},
			b:        NodeBLSMappings{{NodeID: avalanchego.NodeID{1}, Weight: 10}, {NodeID: avalanchego.NodeID{2}, Weight: 20}},
			expected: true,
		},
		{
			name:     "different values",
			a:        NodeBLSMappings{{NodeID: avalanchego.NodeID{1}, Weight: 10}},
			b:        NodeBLSMappings{{NodeID: avalanchego.NodeID{1}, Weight: 99}},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, tt.a.Equal(tt.b))
		})
	}
}

func TestValidatorSetApprovalsFilter(t *testing.T) {
	logger := testutil.MakeLogger(t)
	approvals := ValidatorSetApprovals{
		{NodeID: avalanchego.NodeID{1}, PChainHeight: 10},
		{NodeID: avalanchego.NodeID{2}, PChainHeight: 20},
		{NodeID: avalanchego.NodeID{3}, PChainHeight: 30},
	}

	filtered := approvals.Filter(func(v common.ValidatorSetApproval, _ common.Logger) bool {
		return v.PChainHeight > 15
	}, logger)
	require.Len(t, filtered, 2)
	require.Equal(t, uint64(20), filtered[0].PChainHeight)
	require.Equal(t, uint64(30), filtered[1].PChainHeight)

	// Filter all
	filtered = approvals.Filter(func(common.ValidatorSetApproval, common.Logger) bool {
		return false
	}, logger)
	require.Empty(t, filtered)
}
