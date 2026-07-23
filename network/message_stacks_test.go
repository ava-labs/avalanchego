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
		Enabled:        true,
		MaxMessageSize: 4 * constants.DefaultMaxMessageSize,
		Allowlist:      set.Of(allowlisted),
		Throttler:      DefaultLargeMessageThrottlerConfig(4 * constants.DefaultMaxMessageSize),
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
	require.Equal(t, stacks.Default.OutboundThrottler, defaultStack.OutboundThrottler)

	elevatedStack := stacks.Resolve(allowlisted)
	require.Equal(t, cfg.LargeMessageConfig.MaxMessageSize, elevatedStack.MaxFrameSize)
	require.Equal(t, stacks.Elevated.MessageCreator, elevatedStack.MessageCreator)
	require.Equal(t, stacks.Elevated.InboundMsgThrottler, elevatedStack.InboundMsgThrottler)
	require.Equal(t, stacks.Elevated.OutboundThrottler, elevatedStack.OutboundThrottler)

	// Default and elevated stacks must be distinct when enabled, otherwise the
	// elevated peer would inherit the default frame limit.
	require.NotEqual(t, stacks.Default.MessageCreator, stacks.Elevated.MessageCreator)
	require.NotEqual(t, stacks.Default.MaxFrameSize, stacks.Elevated.MaxFrameSize)
}

// TestMsgCreatorUsesElevatedWhenEnabled verifies the node-wide creator returns
// the large creator when the elevated stack is enabled (so the sender can build
// payloads above the default max size), and the default creator otherwise.
func TestMsgCreatorUsesElevatedWhenEnabled(t *testing.T) {
	vdrs := validators.NewManager()
	cfg, err := NewTestNetworkConfig(prometheus.NewRegistry(), constants.LocalID, vdrs, set.Set[ids.ID]{})
	require.NoError(t, err)
	cfg.LargeMessageConfig = LargeMessageConfig{
		Enabled:        true,
		MaxMessageSize: 4 * constants.DefaultMaxMessageSize,
		Allowlist:      set.Of(ids.GenerateTestNodeID()),
		Throttler:      DefaultLargeMessageThrottlerConfig(4 * constants.DefaultMaxMessageSize),
	}

	stacks, err := newMessageStacks(
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.Validators,
		cfg,
	)
	require.NoError(t, err)
	require.True(t, stacks.elevatedEnabled)
	require.Equal(t, stacks.Elevated.MessageCreator, stacks.MsgCreator())
	require.NotEqual(t, stacks.Default.MessageCreator, stacks.MsgCreator())
}

// TestMsgCreatorUsesDefaultWhenDisabled verifies the node-wide creator falls
// back to the default creator when the elevated stack is disabled.
func TestMsgCreatorUsesDefaultWhenDisabled(t *testing.T) {
	vdrs := validators.NewManager()
	cfg, err := NewTestNetworkConfig(prometheus.NewRegistry(), constants.LocalID, vdrs, set.Set[ids.ID]{})
	require.NoError(t, err)

	stacks, err := newMessageStacks(
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.Validators,
		cfg,
	)
	require.NoError(t, err)
	require.False(t, stacks.elevatedEnabled)
	require.Equal(t, stacks.Default.MessageCreator, stacks.MsgCreator())
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
