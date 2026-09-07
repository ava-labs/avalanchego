// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"fmt"
	"testing"
	"time"

	"github.com/arr4n/shed/testerr"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/logging/loggingtest"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestExecutorHealthCheck(t *testing.T) {
	logs := loggingtest.NewRecorder(logging.Error)
	unhealthy := testerr.As[*saexec.Unhealthy](nil)

	ctx, sut := newSUT(t, 1, options.Func[sutConfig](func(c *sutConfig) {
		c.logger = logs
		c.wantShutdownErr = unhealthy
	}))

	b := sut.runConsensusLoop(t)
	require.NoErrorf(t, b.WaitUntilExecuted(ctx), "%T.WaitUntilExecuted()", b)
	_, err := sut.HealthCheck(ctx)
	require.NoErrorf(t, err, "%T.HealthCheck() after executing first (good) block", sut.rawVM)

	want := testerr.AnyOf(
		// As [saexec.Executor.Enqueue] just pushes to the back of the queue, no
		// checks are performed, but it will eventually cause the queue
		// processor to error.
		nil,
		// Although unlikely, there's a non-zero chance that this block's own
		// failure is surfaced.
		unhealthy,
	)
	if diff := testerr.Diff(sut.rawVM.exec.Enqueue(ctx, b), want); diff != "" {
		t.Fatalf("%T.Enqueue([same block as already accepted]) %s", sut.rawVM.exec, diff)
	}

	t.Run("HealthCheck", func(t *testing.T) {
		want := testerr.As(func(u *saexec.Unhealthy) string {
			if u.Block().Hash() != b.Hash() {
				return fmt.Sprintf("%T.Block().Hash() == %v", u, b.Hash())
			}
			return ""
		})
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			_, err := sut.HealthCheck(ctx)
			if diff := testerr.Diff(err, want); diff != "" {
				c.Errorf("%T.HealthCheck() after enqueue with same block again; %s", sut.rawVM, diff)
			}
		}, 10*time.Second, 10*time.Millisecond)
	})

	t.Run("logs", func(t *testing.T) {
		const key = "block_hash"
		want := []*loggingtest.Record{{
			Level: logging.Error,
			Fields: []zapcore.Field{{
				Key:       key,
				Type:      zapcore.StringerType,
				Interface: b.Hash(),
			}},
		}}
		ignore := cmp.Options{
			cmpopts.IgnoreFields(loggingtest.Record{}, "Msg"),
			cmpopts.IgnoreSliceElements(func(f zapcore.Field) bool {
				return f.Key != key
			}),
		}
		if diff := cmp.Diff(want, logs.Records, ignore); diff != "" {
			t.Errorf("Logged records diff (-want +got):\n%s", diff)
		}
	})
}
