// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"iter"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

type networkedSUTs struct {
	validators, nonValidators []*SUT
}

// newNetworkedSUTs creates a network of SUTs with the specified number of
// validators and non-validators.
//
// Like in production, all nodes are connected to all validators and mark
// themselves as connected. Although non-validators can connect to other
// non-validators, they do not generally attempt to do so in production, so this
// function does not connect them to each other.
func newNetworkedSUTs(tb testing.TB, numValidators, numNonValidators int) *networkedSUTs {
	tb.Helper()

	vdrIDs := set.NewSet[ids.NodeID](numValidators)
	for range numValidators {
		vdrIDs.Add(ids.GenerateTestNodeID())
	}

	net := &networkedSUTs{
		validators:    make([]*SUT, 0, numValidators),
		nonValidators: make([]*SUT, 0, numNonValidators),
	}
	const numAccounts = 1
	for id := range vdrIDs {
		_, sut := newSUT(tb, numAccounts, withNodeID(id), withValidators(vdrIDs))
		net.validators = append(net.validators, sut)
	}
	for range numNonValidators {
		_, sut := newSUT(tb, numAccounts, withValidators(vdrIDs))
		net.nonValidators = append(net.nonValidators, sut)
	}

	// Sanity check that the nodes agree on the genesis block, otherwise the
	// network will exhibit very weird behavior.
	_, expectedSUT := newSUT(tb, numAccounts)
	for selfID, sut := range net.allNodes() {
		require.Equalf(tb, expectedSUT.genesis.ID(), sut.genesis.ID(), "genesis ID for node %s", selfID)
	}

	// Fully connect the validator clique.
	saetest.Connect(tb, net.validators...)
	// Connect each non-validator only to the validators, mirroring production
	// where non-validators don't attempt to connect to each other.
	for _, nv := range net.nonValidators {
		saetest.ConnectTo(tb, nv, net.validators...)
	}
	return net
}

func (net *networkedSUTs) allNodes() iter.Seq2[ids.NodeID, *SUT] {
	return func(yield func(ids.NodeID, *SUT) bool) {
		for _, sut := range net.validators {
			if !yield(sut.NodeID(), sut) {
				return
			}
		}
		for _, sut := range net.nonValidators {
			if !yield(sut.NodeID(), sut) {
				return
			}
		}
	}
}
