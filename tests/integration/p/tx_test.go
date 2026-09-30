// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"
)

// TestGetTxStatus checks the status of unknown, committed, and dropped
// transactions before and after reopening the database.
func TestGetTxStatus(t *testing.T) {
	tc, v, wallet, key := newVMWithWallet(t)
	ctx := tc.DefaultContext()
	committed := pchain.AddDelegator(tc, wallet, v.Node, []pchain.Node{v.Node}, genesistest.DefaultNodeIDs[0], time.Time{}, key.Address())
	// A delegation to an unknown validator fails verification when issued.
	dropped, _ := pchain.IssueAddDelegatorTx(ctx, wallet, v.Node, ids.GenerateTestNodeID(), v.ChainTime().Add(time.Hour), key.Address())
	require.NotNil(t, dropped)

	cases := []struct {
		name            string
		txID            ids.ID
		want            status.Status
		wantAfterReopen status.Status
	}{
		{name: "unknown", txID: ids.GenerateTestID(), want: status.Unknown, wantAfterReopen: status.Unknown},
		{name: "committed", txID: committed.ID(), want: status.Committed, wantAfterReopen: status.Committed},
		// Drop reasons live in memory, so a reopened node no longer knows the tx.
		{name: "dropped", txID: dropped.ID(), want: status.Dropped, wantAfterReopen: status.Unknown},
	}
	for _, c := range cases {
		require.Equal(t, c.want, getStatus(t, v, c.txID).Status, c.name)
	}

	v.Reopen()

	for _, c := range cases {
		require.Equal(t, c.wantAfterReopen, getStatus(t, v, c.txID).Status, c.name)
	}
}

// TestGetTxStatusProcessing checks the status of a transaction that is
// accepted into the mempool but not yet in a block.
func TestGetTxStatusProcessing(t *testing.T) {
	tc, v, wallet, key := newVMWithWallet(t)

	resume := v.PauseAcceptance()
	tx, err := pchain.IssueAddDelegatorTx(tc.DefaultContext(), wallet, v.Node, genesistest.DefaultNodeIDs[0], time.Time{}, key.Address(), common.WithAssumeDecided())
	require.NoError(t, err)
	require.Equal(t, status.Processing, getStatus(t, v, tx.ID()).Status)

	// Fails unless the pending transaction commits once acceptance resumes.
	resume()
	pchain.WaitForTxCommitted(tc, []pchain.Node{v.Node}, tx.ID())
}

func getStatus(t *testing.T, v *pchain.VM, txID ids.ID) pchain.NodeTxStatus {
	res := pchain.GetTxStatus(t.Context(), v.Node, txID)
	require.NoError(t, res.Err)
	return res
}
