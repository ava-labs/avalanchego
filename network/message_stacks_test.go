// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestMessageStacksResolve(t *testing.T) {
	allowlisted := ids.GenerateTestNodeID()
	other := ids.GenerateTestNodeID()

	vdrs := validators.NewManager()
	cfg, err := NewTestNetworkConfig(prometheus.NewRegistry(), constants.LocalID, vdrs, set.Set[ids.ID]{})
	require.NoError(t, err)
	cfg.LargeMessageConfig = LargeMessageConfig{
		MaxMessageSize: 4 * constants.DefaultMaxMessageSize,
		Allowlist:      set.Of(allowlisted),
	}

	stacks, err := newMessageStacks(
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.Validators,
		cfg,
	)
	require.NoError(t, err)
	require.True(t, stacks.elevatedEnabled)

	defaultStack := stacks.Resolve(other)
	require.Equal(t, uint32(constants.DefaultMaxMessageSize), defaultStack.MaxFrameSize)
	require.Equal(t, stacks.Default.MessageCreator, defaultStack.MessageCreator)
	require.Equal(t, stacks.Default.InboundMsgThrottler, defaultStack.InboundMsgThrottler)

	elevatedStack := stacks.Resolve(allowlisted)
	require.Equal(t, cfg.LargeMessageConfig.MaxMessageSize, elevatedStack.MaxFrameSize)
	require.Equal(t, stacks.Elevated.MessageCreator, elevatedStack.MessageCreator)
	require.Equal(t, stacks.Elevated.InboundMsgThrottler, elevatedStack.InboundMsgThrottler)
}

func TestMessageStacksDisabledWhenAllowlistEmpty(t *testing.T) {
	vdrs := validators.NewManager()
	cfg, err := NewTestNetworkConfig(prometheus.NewRegistry(), constants.LocalID, vdrs, set.Set[ids.ID]{})
	require.NoError(t, err)
	cfg.LargeMessageConfig = LargeMessageConfig{
		MaxMessageSize: 4 * constants.DefaultMaxMessageSize,
		Allowlist:      set.Set[ids.NodeID]{},
	}

	stacks, err := newMessageStacks(
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.Validators,
		cfg,
	)
	require.NoError(t, err)
	require.False(t, stacks.elevatedEnabled)
	require.Equal(t, stacks.Default, stacks.Resolve(ids.GenerateTestNodeID()))
}
