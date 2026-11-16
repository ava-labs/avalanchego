// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metadata

import (
	"errors"
	"testing"

	"github.com/ava-labs/simplex/avalanchego"
	"github.com/ava-labs/simplex/common"
	"github.com/ava-labs/simplex/testutil"
	"github.com/stretchr/testify/require"
)

func makeNodeID(seed byte) avalanchego.NodeID {
	var n avalanchego.NodeID
	n[0] = seed
	return n
}

func makeValidators(n int) NodeBLSMappings {
	vdrs := make(NodeBLSMappings, n)
	for i := 0; i < n; i++ {
		vdrs[i] = NodeBLSMapping{
			NodeID: makeNodeID(byte(i + 1)),
			BLSKey: []byte{byte(i + 1)},
			Weight: 1,
		}
	}
	return vdrs
}

// TestAggregatePubKeysForBitmaskRejectsWideBitmask ensures an approvals bitmask carrying
// bits beyond the validator set is rejected. Such bits index no validator, so they convey
// no approval and are ignored when keys are aggregated and when the subset is selected --
// but they are free for a proposer to add and make every other node carry the width.
func TestAggregatePubKeysForBitmaskRejectsWideBitmask(t *testing.T) {
	sm := &StateMachine{Config: &Config{KeyAggregator: &keyAggregator{}}}
	validators := makeValidators(4)

	// Every validator approving is the widest legitimate bitmask.
	all := avalanchego.BitmaskFromBytes(nil)
	for i := range validators {
		all.Add(i)
	}
	aggPK, err := sm.aggregatePubKeysForBitmask(all.Bytes(), validators)
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2, 3, 4}, aggPK)

	// One bit past the end of the validator set.
	tooWide := all.Clone()
	tooWide.Add(len(validators))
	_, err = sm.aggregatePubKeysForBitmask(tooWide.Bytes(), validators)
	require.ErrorIs(t, err, errApprovalsBitmaskTooWide)

	// A single very high bit, which is the shape that made bitmask operations expensive.
	huge := avalanchego.BitmaskFromBytes(nil)
	huge.Add(1 << 20)
	_, err = sm.aggregatePubKeysForBitmask(huge.Bytes(), validators)
	require.ErrorIs(t, err, errApprovalsBitmaskTooWide)
}

func TestApprovalStoreHandleApproval(t *testing.T) {
	for _, tc := range []struct {
		name       string
		validators int
		sigErr     error
		// approvals are handed to the store in order; node i is makeNodeID(i+1) from makeValidators.
		approvals []common.ValidatorSetApproval
		// expected indexes the approvals above that Approvals() should return afterwards, in any order.
		expected []int
	}{
		{
			name:       "approval from unknown node is dropped",
			validators: 3,
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(99), PChainHeight: 1, Signature: signApproval(1, common.Digest{})},
			},
		},
		{
			name:       "approval with invalid signature is dropped",
			validators: 3,
			sigErr:     errors.New("bad sig"),
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(1), PChainHeight: 1, Signature: signApproval(1, common.Digest{})},
			},
		},
		{
			name:       "valid approval is stored",
			validators: 3,
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(1), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
			},
			expected: []int{0},
		},
		{
			name:       "duplicate approval is stored once",
			validators: 3,
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(1), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
				{NodeID: makeNodeID(1), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
			},
			expected: []int{1},
		},
		{
			name:       "latest approval from a node replaces the earlier one",
			validators: 3,
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(1), PChainHeight: 7, AuxInfoDigest: common.Digest{0xAA}, Signature: signApproval(7, common.Digest{0xAA})},
				{NodeID: makeNodeID(1), PChainHeight: 8, AuxInfoDigest: common.Digest{0xBB}, Signature: signApproval(8, common.Digest{0xBB})},
			},
			expected: []int{1},
		},
		{
			name:       "approvals from different nodes coexist",
			validators: 3,
			approvals: []common.ValidatorSetApproval{
				{NodeID: makeNodeID(1), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
				{NodeID: makeNodeID(2), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
				{NodeID: makeNodeID(3), PChainHeight: 7, Signature: signApproval(7, common.Digest{})},
			},
			expected: []int{0, 1, 2},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			as := NewApprovalStore(&signatureVerifier{err: tc.sigErr}, makeValidators(tc.validators), testutil.MakeLogger(t))
			for i := range tc.approvals {
				as.HandleApproval(&tc.approvals[i])
			}
			expected := make([]common.ValidatorSetApproval, 0, len(tc.expected))
			for _, i := range tc.expected {
				expected = append(expected, tc.approvals[i])
			}
			require.ElementsMatch(t, expected, []common.ValidatorSetApproval(as.Approvals()))
		})
	}
}
