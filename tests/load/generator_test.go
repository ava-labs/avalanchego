// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type mockTest struct {
	iterationDuration time.Duration
	executedCount     int
}

func (m *mockTest) Run(tc tests.TestContext, _ *Wallet) {
	ctx := tc.GetDefaultContextParent()
	select {
	case <-time.After(m.iterationDuration):
		m.executedCount++
	case <-ctx.Done():
		tc.Fatal(ctx.Err())
	}
}

func TestLoadGenerator_Run_LoadTimeoutDoesNotCancelInFlightWorker(t *testing.T) {
	require := require.New(t)

	mock := &mockTest{
		iterationDuration: 50 * time.Millisecond,
	}

	generator := LoadGenerator{
		wallets: []*Wallet{{}},
		test:    mock,
	}

	// loadTimeout is 30ms (fires while the 50ms iteration is in-flight).
	// testTimeout is 500ms.
	// The in-flight iteration should finish without ctx.Done() firing in mockTest.Run.
	start := time.Now()
	generator.Run(
		context.Background(),
		logging.NoLog{},
		30*time.Millisecond,
		500*time.Millisecond,
	)
	elapsed := time.Since(start)

	require.GreaterOrEqual(elapsed, 50*time.Millisecond)
	require.Equal(1, mock.executedCount)
}
