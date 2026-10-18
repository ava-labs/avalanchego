// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type healthErrorRuntime struct {
	NodeRuntime
	err error
}

func (r *healthErrorRuntime) IsHealthy(context.Context) (bool, error) {
	return false, r.err
}

func TestWaitForHealthyReturnsWhenNodeStopped(t *testing.T) {
	node := &Node{
		runtime: &healthErrorRuntime{err: errNotRunning},
		network: &Network{log: logging.NoLog{}},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	err := node.WaitForHealthy(ctx)
	require.ErrorIs(t, err, errNotRunning)
}
