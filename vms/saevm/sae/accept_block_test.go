// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/rand/v2"
	"os"
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
	// TODO(JonathanOppenheimer): determine whether this test is actually flaky
	// or whether there's a bug in the test. This test is enabled in Bazel and
	// disabled in go test.
	if os.Getenv("SAEVM_TEST_ACCEPT_BLOCK") == "" {
		t.Skip("FLAKY: set SAEVM_TEST_ACCEPT_BLOCK to run")
	}

	// We use a generous timeout because GC finalizers from previous tests take
	// a long time to run in resource constrained environments.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		runtime.GC()
		require.Zero(t, blocks.InMemoryBlockCount(), "initial in-memory block count")
	}, 5*time.Second, 50*time.Millisecond)

	opt, vmTime := withVMTime(t, time.Unix(saeparams.TauSeconds, 0))

	ctx, sut := newSUT(t, 1, opt)
	// Causes [VM.AcceptBlock] to wait until the block has executed.
	require.NoError(t, sut.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")

	unsettled := []*blocks.Block{sut.genesis}
	sut.genesis = nil // allow it to be GCd when appropriate

	rng := rand.New(rand.NewPCG(0, 0))
	for range 100 {
		ffMillis := 100 + rng.IntN(1000*(1+saeparams.TauSeconds))
		vmTime.advance(time.Millisecond * time.Duration(ffMillis))

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

		assert.EventuallyWithT(t, func(t *assert.CollectT) {
			runtime.GC()
			require.Equal(t, int64(wantInMemory.Len()), blocks.InMemoryBlockCount(), "in-memory block count")
		}, 100*time.Millisecond, time.Millisecond)
	}
}
