// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fuzz

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/platform"

	pvmproposer "github.com/ava-labs/avalanchego/vms/proposervm/proposer"
)

// TestInnerBlock checks that a proposed block is built as an honest builder
// builds one on its parent: one height above it, at the parent's time or
// now, whichever is later, and as a proposal block led by its first
// proposer-only tx.
func TestInnerBlock(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	parent := fakeParent(t, now.Add(-time.Minute), 7)
	transfer := initialized(t, &platform.BaseTx{BaseTx: avax.BaseTx{NetworkID: constants.UnitTestID}})
	reward := initialized(t, &platform.RewardValidatorTx{TxID: ids.GenerateTestID()})

	t.Run("standard", func(t *testing.T) {
		blk, err := innerBlock(parent, []*platform.Tx{transfer}, now)
		require.NoError(t, err)
		require.IsType(t, &platform.BanffStandardBlock{}, blk)
		require.Equal(t, parent.inner.ID(), blk.Parent())
		require.Equal(t, parent.inner.Height()+1, blk.Height())
		require.Equal(t, now, blk.(platform.BanffBlock).Timestamp())
		require.Equal(t, []*platform.Tx{transfer}, blk.Txs())
	})

	t.Run("proposal", func(t *testing.T) {
		blk, err := innerBlock(parent, []*platform.Tx{transfer, reward}, now)
		require.NoError(t, err)
		require.IsType(t, &platform.BanffProposalBlock{}, blk)
		require.Equal(t, []*platform.Tx{transfer, reward}, blk.Txs(), "the decision txs, then the proposal tx")
	})

	t.Run("parent_after_now", func(t *testing.T) {
		later := fakeParent(t, now.Add(time.Minute), 7)
		blk, err := innerBlock(later, nil, now)
		require.NoError(t, err)
		require.Equal(t, now.Add(time.Minute), blk.(platform.BanffBlock).Timestamp())
	})
}

// TestOwnSlot checks that the proposer takes the slot that holds now if it is
// that slot's expected proposer, though the slot started before now, or the
// next slot if that starts within the wait, and else no slot.
func TestOwnSlot(t *testing.T) {
	window, err := pvmproposer.WindowDurationOf(testUpgrades())
	require.NoError(t, err)
	w := time.Duration(window)
	parentTime := time.Unix(1_000_000, 0)
	p := &proposer{key: validatorKey{nodeID: ids.GenerateTestNodeID()}}
	other := ids.GenerateTestNodeID()

	tests := []struct {
		name    string
		own     []uint64 // the slots that expect the proposer
		now     time.Time
		wait    time.Duration
		want    time.Time
		wantErr error
	}{
		{name: "current_slot", own: []uint64{2}, now: parentTime.Add(2*w + w/2), want: parentTime.Add(2 * w)},
		{name: "next_slot_no_wait", own: []uint64{3}, now: parentTime.Add(3*w - w/20), wantErr: errNoSlot},
		{name: "next_slot_within_wait", own: []uint64{3}, now: parentTime.Add(3*w - w/20), wait: w / 10, want: parentTime.Add(3 * w)},
		{name: "next_slot_after_wait", own: []uint64{3}, now: parentTime.Add(3*w - w/5), wait: w / 10, wantErr: errNoSlot},
		{name: "first_slot_after_now", own: []uint64{0}, now: parentTime.Add(-w / 20), wait: w / 10, want: parentTime},
		{name: "none", now: parentTime.Add(2*w + w/2), wait: w / 10, wantErr: errNoSlot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expected := func(slot uint64) (ids.NodeID, error) {
				if slices.Contains(tt.own, slot) {
					return p.key.nodeID, nil
				}
				return other, nil
			}
			got, err := p.ownSlot(parentTime, tt.now, tt.wait, expected)
			require.ErrorIs(t, err, tt.wantErr)
			require.Equal(t, tt.want, got)
		})
	}
}

// fakeParent returns a parent whose P-Chain block is an empty standard block
// at height and time at.
func fakeParent(t testing.TB, at time.Time, height uint64) parentBlock {
	t.Helper()

	inner, err := platform.NewBanffStandardBlock(at, ids.GenerateTestID(), height, nil)
	require.NoError(t, err)

	return parentBlock{id: ids.GenerateTestID(), inner: inner, time: at}
}

// initialized returns utx in a tx with no credentials.
func initialized(t *testing.T, utx platform.UnsignedTx) *platform.Tx {
	t.Helper()

	tx := &platform.Tx{Unsigned: utx}
	require.NoError(t, tx.Initialize(platform.Codec))

	return tx
}
