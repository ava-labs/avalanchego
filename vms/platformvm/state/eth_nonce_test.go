// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

// Diff.UTXOIDs reports a UTXO that exists in the parent and is re-added in the
// diff twice. That is allowed, but it is why every caller deduplicates: summing
// a duplicate's amount while deleting it once would mint AVAX.
func TestDiffUTXOIDsMayContainDuplicates(t *testing.T) {
	require := require.New(t)

	s := newTestState(t, memdb.New())
	addr := ids.GenerateTestShortID()
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{TxID: ids.GenerateTestID(), OutputIndex: 0},
		Asset:  avax.Asset{ID: ids.GenerateTestID()},
		Out: &secp256k1fx.TransferOutput{
			Amt: 12345,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{addr},
			},
		},
	}
	s.AddUTXO(utxo)
	s.SetHeight(1)
	require.NoError(s.Commit())

	d, err := NewDiffOn(s, StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	d.AddUTXO(utxo)

	merged, err := d.UTXOIDs(addr.Bytes(), ids.Empty, 100)
	require.NoError(err)
	require.Len(merged, 2)
	require.Equal(merged[0], merged[1])
}

// The nonce state round-trips through a diff and its parent.
func TestNonceStateThroughDiff(t *testing.T) {
	require := require.New(t)

	s := newTestState(t, memdb.New())
	addr := ids.GenerateTestShortID()

	next, err := s.GetNextNonce(addr)
	require.NoError(err)
	require.Zero(next)

	d, err := NewDiffOn(s, StakerAdditionAfterDeletionForbidden)
	require.NoError(err)
	d.SetNextNonce(addr, 7)

	next, err = d.GetNextNonce(addr)
	require.NoError(err)
	require.Equal(uint64(7), next)

	// The parent only sees it after Apply.
	next, err = s.GetNextNonce(addr)
	require.NoError(err)
	require.Zero(next)

	require.NoError(d.Apply(s))
	s.SetHeight(1)
	require.NoError(s.Commit())

	next, err = s.GetNextNonce(addr)
	require.NoError(err)
	require.Equal(uint64(7), next)
}
