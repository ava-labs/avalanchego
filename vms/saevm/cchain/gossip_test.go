// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"testing"

	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// TestPushGossip verifies that a cross-chain transaction issued to an API node
// is push-gossiped to a validator for block building.
func TestPushGossip(t *testing.T) {
	var (
		sk        = txtest.NewKey(t)
		withAlloc = options.Func[sutConfig](func(c *sutConfig) {
			c.genesis.Alloc = saetest.MaxAllocFor(sk.EthAddress())
		})
		vdrID = ids.GenerateTestNodeID()
		vdrs  = set.Of(vdrID)
	)
	api := newSUT(t, withAlloc, withValidators(vdrs))
	vdr := newSUT(t, withAlloc, withNodeID(vdrID), withValidators(vdrs))
	saetest.Connect(t, api, vdr)

	w := newWallet(sk, api.snowCtx, api.Client)
	stx := w.newMinimalTx(t)
	require.NoErrorf(t, api.IssueTx(t.Context(), stx), "%T.IssueTx()", api.Client)

	blk := vdr.runConsensusLoop(t)
	if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T built by validator after gossip (-want +got):\n%s", blk, diff)
	}
}

// TestPullGossip verifies that a validator will share a cross-chain transaction
// via pull gossip to another connected validator.
//
// The API node is only transitively connected to vdrB, so vdrB can only learn
// about the transaction by pulling it from vdrA.
func TestPullGossip(t *testing.T) {
	var (
		sk        = txtest.NewKey(t)
		withAlloc = options.Func[sutConfig](func(c *sutConfig) {
			c.genesis.Alloc = saetest.MaxAllocFor(sk.EthAddress())
		})
		vdrIDA = ids.GenerateTestNodeID()
		vdrIDB = ids.GenerateTestNodeID()
		vdrs   = set.Of(vdrIDA, vdrIDB)
	)
	api := newSUT(t, withAlloc, withValidators(vdrs))
	vdrA := newSUT(t, withAlloc, withNodeID(vdrIDA), withValidators(vdrs))
	vdrB := newSUT(t, withAlloc, withNodeID(vdrIDB), withValidators(vdrs))
	saetest.Connect(t, api, vdrA)
	saetest.Connect(t, vdrA, vdrB)

	w := newWallet(sk, api.snowCtx, api.Client)
	stx := w.newMinimalTx(t)
	require.NoErrorf(t, api.IssueTx(t.Context(), stx), "%T.IssueTx()", api.Client)

	blk := vdrB.runConsensusLoop(t)
	if diff := cmp.Diff([]*tx.Tx{stx}, blockTxs(t, blk), txtest.CmpOpt()); diff != "" {
		t.Errorf("%T built by vdrB after gossip (-want +got):\n%s", blk, diff)
	}
}
