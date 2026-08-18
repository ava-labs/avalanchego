// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx/txtest"
	"github.com/ava-labs/avalanchego/vms/saevm/sae"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"
)

// vmHealthDetails and stateSyncDetails mirror the JSON that operators and
// tooling consume from the node's health API. They are declared separately from
// [sae.Health] and [sae.StateSync] so that renaming a reported field, which is a
// breaking change, fails this test.
type vmHealthDetails struct {
	State       string            `json:"state"`
	StateScheme string            `json:"stateScheme"`
	StateSync   *stateSyncDetails `json:"stateSync"`
}

type stateSyncDetails struct {
	Status        string `json:"status"`
	SummaryHeight uint64 `json:"summaryHeight"`
	SummaryHash   string `json:"summaryHash"`
	Error         string `json:"error"`
}

// health returns the VM's health details as the health API serializes them.
func (s *SUT) health(ctx context.Context, tb testing.TB) vmHealthDetails {
	tb.Helper()

	details, err := s.HealthCheck(ctx)
	require.NoErrorf(tb, err, "%T.HealthCheck()", s.VM)

	// Round-trip through JSON to assert on what the health API actually serves.
	rawDetails, err := json.Marshal(details)
	require.NoErrorf(tb, err, "marshaling %T.HealthCheck() details", s.VM)
	var health vmHealthDetails
	require.NoErrorf(tb, json.Unmarshal(rawDetails, &health), "unmarshaling health details %s", rawDetails)
	return health
}

// TestHealthCheckWithoutStateSync asserts the details reported by a node that
// never state syncs, distinguishing state sync being configured off from a node
// that has yet to accept a summary.
func TestHealthCheckWithoutStateSync(t *testing.T) {
	tests := []struct {
		name       string
		opts       []sutOption
		wantStatus string
	}{
		{
			name:       "enabled_but_never_offered_a_summary",
			wantStatus: sae.StateSyncNotStarted,
		},
		{
			name:       "disabled",
			opts:       []sutOption{withStateSyncDisabled()},
			wantStatus: sae.StateSyncDisabled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, sut := newSUT(t, tt.opts...)

			require.Equal(t, vmHealthDetails{
				State:       sae.HealthStateNormalOp,
				StateScheme: rawdb.HashScheme,
				StateSync:   &stateSyncDetails{Status: tt.wantStatus},
			}, sut.health(ctx, t), "health details")
		})
	}
}

// TestHealthCheckStateSync asserts the details reported across a state sync,
// from before a summary is accepted through the sync completing and the node
// entering normal operation.
//
// The VM MUST be health-checkable throughout: the engine health-checks it while
// state syncing, before [VM.SetState] has constructed the [sae.VM].
func TestHealthCheckStateSync(t *testing.T) {
	const commitInterval = 8

	key := txtest.NewKey(t)
	ethW := saetest.NewUNSAFEWallet(t, 1, types.LatestSigner(saetest.ChainConfig()))
	timeOpt, _ := withVMTime(testStartTime)
	sharedOpts := []sutOption{
		timeOpt,
		withMaxAllocFor(key.EthAddress(), ethW.Addresses()[0]),
		withCommitInterval(commitInterval),
	}
	srcCtx, src := newSUT(t, sharedOpts...)
	w := newWallet(key, src.ctx, src.Client)

	// Fill the chain past the first commit boundary so that the source can
	// serve a summary.
	src.produceBlocks(srcCtx, t, w, ethW, commitInterval+1)

	ctx, dst := newSUT(t, append(sharedOpts, withState(snow.StateSyncing))...)
	saetest.ConnectTo(t, dst, src)

	// No summary has been accepted yet, so the sync is reported as not started
	// rather than as in progress.
	require.Equal(t, vmHealthDetails{
		State:       sae.HealthStateStateSyncing,
		StateScheme: rawdb.HashScheme,
		StateSync:   &stateSyncDetails{Status: sae.StateSyncNotStarted},
	}, dst.health(ctx, t), "health details while awaiting a summary")

	summaryHeight := startStateSync(ctx, t, src, dst)
	require.Equal(t, uint64(commitInterval), summaryHeight, "summary at last commit boundary")
	awaitStateSync(ctx, t, dst)

	// The synced summary's block is reported, and is the source's block at the
	// summary height, which is what the engine offered.
	summaryBlock, err := src.GetBlockIDAtHeight(srcCtx, summaryHeight)
	require.NoErrorf(t, err, "%T.GetBlockIDAtHeight(%d)", src.VM, summaryHeight)
	require.NotEqual(t, ids.Empty, summaryBlock, "source block at summary height")

	wantSynced := &stateSyncDetails{
		Status:        sae.StateSyncCompleted,
		SummaryHeight: summaryHeight,
		SummaryHash:   common.Hash(summaryBlock).Hex(),
	}
	require.Equal(t, vmHealthDetails{
		State:       sae.HealthStateStateSyncing,
		StateScheme: rawdb.HashScheme,
		StateSync:   wantSynced,
	}, dst.health(ctx, t), "health details after the sync completes")

	// The completed sync stays reported once the node is operating normally, so
	// an operator can still tell that the node's state below the summary height
	// was synced rather than executed.
	dst.bootstrapFrom(ctx, t, src, summaryHeight)
	require.Equal(t, vmHealthDetails{
		State:       sae.HealthStateNormalOp,
		StateScheme: rawdb.HashScheme,
		StateSync:   wantSynced,
	}, dst.health(ctx, t), "health details after bootstrapping")
}
