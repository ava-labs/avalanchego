// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/rand/v2"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

func TestAcceptBlock(t *testing.T) {
	// [blocks.InMemoryBlockCount] is global to the process, and blocks from other
	// tests can stay in memory indefinitely, so this test needs its own process.
	const subprocessEnv = "SAEVM_TEST_ACCEPT_BLOCK_SUBPROCESS"
	if os.Getenv(subprocessEnv) == "" {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestAcceptBlock$") //#nosec G204,G702 -- Runs this same test binary
		cmd.Env = append(os.Environ(), subprocessEnv+"=1")
		out, err := cmd.CombinedOutput()
		require.NoErrorf(t, err, "%s", out)
		return
	}

	require.Zero(t, blocks.InMemoryBlockCount(), "initial in-memory block count")

	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	ctx, sut := newSUT(t, 1, opt)
	// Causes [VM.AcceptBlock] to wait until the block has executed.
	require.NoError(t, sut.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")

	unsettled := []*blocks.Block{sut.genesis}
	sut.genesis = nil // allow it to be GCd when appropriate

	rng := rand.New(rand.NewPCG(0, 0)) //#nosec G404 -- Reproducibility is useful for tests
	for range 100 {
		ffMillis := 100 + rng.IntN(1000*(1+saeparams.TauSeconds))
		vmTime.Advance(time.Millisecond * time.Duration(ffMillis))

		b := sut.runConsensusLoop(t)
		unsettled = append(unsettled, b)
		sut.assertBlockHashInvariants(ctx, t)

		lastSettled := b.LastSettled().Height()
		var wantInMemory set.Set[uint64]
		for i, bb := range unsettled {
			switch {
			case bb == nil: // settled earlier
			case bb.Settled():
				unsettled[i] = nil
				require.LessOrEqual(t, bb.Height(), lastSettled, "height of settled block")

			default:
				require.Greater(t, bb.Height(), lastSettled, "height of unsettled block")
				wantInMemory.Add(
					bb.Height(),
					bb.ParentBlock().Height(),
					bb.LastSettled().Height(),
				)
			}
		}

		// The count only drops after a GC cycle and the cleanups that it queues.
		// We use a generous timeout because, in resource constrained environments,
		// [runtime.GC] alone can take hundreds of milliseconds.
		require.EventuallyWithT(t, func(t *assert.CollectT) {
			runtime.GC()
			require.Equal(t, int64(wantInMemory.Len()), blocks.InMemoryBlockCount(), "in-memory block count")
		}, 5*time.Second, time.Millisecond)
	}
}
