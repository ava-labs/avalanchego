// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// assertTxBloomContains asserts that the transaction bloom contains the given
// transaction IDs.
func (s *SUT) assertTxBloomContains(tb testing.TB, txIDs ...ids.ID) {
	tb.Helper()

	filter, salt := s.gossipSet.BloomFilter()
	for i, txID := range txIDs {
		assert.Truef(tb, bloom.Contains(filter, txID[:], salt[:]), "bloom filter should contain %s (%d)", txID, i)
	}
}

// assertTxBloomEmpty asserts that the transaction bloom is empty.
//
// Asserting the bloom doesn't contain a specific transaction could flake.
func (s *SUT) assertTxBloomEmpty(tb testing.TB) {
	tb.Helper()

	filter, _ := s.gossipSet.BloomFilter()
	assert.Zero(tb, filter.Count(), "bloom filter should be empty")
}

// TestPushGossip verifies that a cross-chain transaction issued to an API node
// is push-gossiped to a validator for block building.
func TestPushGossip(t *testing.T) {
	var (
		sk        = txtest.NewKey(t)
		withAlloc = withMaxAllocFor(sk.EthAddress())
		vdrID     = ids.GenerateTestNodeID()
		vdrs      = set.Of(vdrID)
	)
	apiCtx, api := newSUT(t, withAlloc, withValidators(vdrs))
	vdrCtx, vdr := newSUT(t, withAlloc, withNodeID(vdrID), withValidators(vdrs))
	saetest.Connect(t, api, vdr)

	w := newWallet(sk, api.ctx, api.Client)
	stx := w.newMinimalTx(t)
	require.NoErrorf(t, api.IssueTx(apiCtx, stx), "%T.IssueTx()", api.Client)
	api.assertTxBloomContains(t, stx.ID())

	blk := vdr.runConsensusLoop(vdrCtx, t)
	if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T built by validator after gossip (-want +got):\n%s", blk, diff)
	}
}

// TestPullGossip verifies that a validator will share a cross-chain transaction
// via pull gossip to another connected validator.
func TestPullGossip(t *testing.T) {
	var (
		sk        = txtest.NewKey(t)
		withAlloc = withMaxAllocFor(sk.EthAddress())
		vdrIDA    = ids.GenerateTestNodeID()
		vdrIDB    = ids.GenerateTestNodeID()
		vdrs      = set.Of(vdrIDA, vdrIDB)
	)
	apiCtx, api := newSUT(t, withAlloc, withValidators(vdrs))
	_, vdrA := newSUT(t, withAlloc, withNodeID(vdrIDA), withValidators(vdrs))
	vdrBCtx, vdrB := newSUT(t, withAlloc, withNodeID(vdrIDB), withValidators(vdrs))
	saetest.ConnectTo(t, api, vdrA) // api is not connected to vdrB
	saetest.ConnectTo(t, vdrA, vdrB)

	w := newWallet(sk, api.ctx, api.Client)
	stx := w.newMinimalTx(t)
	require.NoErrorf(t, api.IssueTx(apiCtx, stx), "%T.IssueTx()", api.Client)
	api.assertTxBloomContains(t, stx.ID())

	// Because vdrB isn't connected to api, vdrB can only learn about the
	// transaction by pulling it from vdrA.
	blk := vdrB.runConsensusLoop(vdrBCtx, t)
	if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T built by vdrB after gossip (-want +got):\n%s", blk, diff)
	}
}

// TestPushGossipAfterPullGossip verifies that a node which previously received
// a cross-chain transaction via gossip will share it via push gossip to a
// connected validator.
func TestPushGossipAfterPullGossip(t *testing.T) {
	var (
		sk        = txtest.NewKey(t)
		withAlloc = withMaxAllocFor(sk.EthAddress())
		vdrIDA    = ids.GenerateTestNodeID()
		vdrIDB    = ids.GenerateTestNodeID()
		vdrs      = set.Of(vdrIDA, vdrIDB)
	)
	apiCtx, api := newSUT(t, withAlloc, withValidators(vdrs))
	vdrACtx, vdrA := newSUT(t, withAlloc, withNodeID(vdrIDA), withValidators(vdrs))
	vdrBCtx, vdrB := newSUT(t, withAlloc, withNodeID(vdrIDB)) // vdrB doesn't consider vdrA a validator
	saetest.ConnectTo(t, api, vdrA)                           // api is not connected to vdrB
	saetest.ConnectTo(t, vdrA, vdrB)

	w := newWallet(sk, api.ctx, api.Client)
	stx := w.newMinimalTx(t)
	require.NoErrorf(t, api.IssueTx(apiCtx, stx), "%T.IssueTx()", api.Client)
	api.assertTxBloomContains(t, stx.ID())

	// Ensure vdrA learned about stx before we reissue the tx so we don't race
	// with the normal issuance path.
	vdrA.waitForPendingTxs(vdrACtx, t)

	// Because vdrB doesn't consider vdrA a validator and isn't connected to
	// api, vdrB can only learn about the transaction if vdrA pushes it.
	vdrB.assertTxBloomEmpty(t)

	require.NoErrorf(t, vdrA.IssueTx(vdrACtx, stx), "%T.IssueTx()", vdrA.VM)
	vdrA.assertTxBloomContains(t, stx.ID())

	blk := vdrB.runConsensusLoop(vdrBCtx, t)
	if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T built by vdrB after gossip (-want +got):\n%s", blk, diff)
	}
}
