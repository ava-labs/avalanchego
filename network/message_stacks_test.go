// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/throttling"
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
		MaxMessageSize: 4 * constants.DefaultMaxMessageSize,
		Allowlist:      set.Of(ids.GenerateTestNodeID()),
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

func TestElevatedInboundThrottlerConfig(t *testing.T) {
	const (
		maxSize      = uint64(4 * constants.DefaultMaxMessageSize)
		allowlistLen = 3
	)
	base := throttling.InboundMsgThrottlerConfig{
		MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
			VdrAllocSize:        constants.DefaultInboundThrottlerVdrAllocSize,
			AtLargeAllocSize:    constants.DefaultInboundThrottlerAtLargeAllocSize,
			NodeMaxAtLargeBytes: constants.DefaultInboundThrottlerNodeMaxAtLargeBytes,
		},
		BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
			RefillRate:   constants.DefaultInboundThrottlerBandwidthRefillRate,
			MaxBurstSize: constants.DefaultInboundThrottlerBandwidthMaxBurstSize,
		},
		MaxProcessingMsgsPerNode: constants.DefaultInboundThrottlerMaxProcessingMsgsPerNode,
	}

	got := elevatedInboundThrottlerConfig(base, maxSize, allowlistLen)

	// Overridden so allowlisted peers may send/receive up to maxSize.
	require.Equal(t, maxSize, got.MsgByteThrottlerConfig.NodeMaxAtLargeBytes)
	require.Equal(t, maxSize, got.BandwidthThrottlerConfig.MaxBurstSize)
	require.Equal(t, maxSize*allowlistLen, got.MsgByteThrottlerConfig.AtLargeAllocSize)

	// Everything else is preserved from the base config.
	require.Equal(t, base.MsgByteThrottlerConfig.VdrAllocSize, got.MsgByteThrottlerConfig.VdrAllocSize)
	require.Equal(t, base.BandwidthThrottlerConfig.RefillRate, got.BandwidthThrottlerConfig.RefillRate)
	require.Equal(t, base.MaxProcessingMsgsPerNode, got.MaxProcessingMsgsPerNode)

	// The base config must not be mutated.
	require.Equal(t, constants.DefaultInboundThrottlerNodeMaxAtLargeBytes, int(base.MsgByteThrottlerConfig.NodeMaxAtLargeBytes))
}

func TestElevatedOutboundThrottlerConfig(t *testing.T) {
	const (
		maxSize      = uint64(4 * constants.DefaultMaxMessageSize)
		allowlistLen = 3
	)
	base := throttling.MsgByteThrottlerConfig{
		VdrAllocSize:        constants.DefaultOutboundThrottlerVdrAllocSize,
		AtLargeAllocSize:    constants.DefaultOutboundThrottlerAtLargeAllocSize,
		NodeMaxAtLargeBytes: constants.DefaultOutboundThrottlerNodeMaxAtLargeBytes,
	}

	got := elevatedOutboundThrottlerConfig(base, maxSize, allowlistLen)

	require.Equal(t, maxSize, got.NodeMaxAtLargeBytes)
	require.Equal(t, maxSize*allowlistLen, got.AtLargeAllocSize)
	require.Equal(t, base.VdrAllocSize, got.VdrAllocSize)

	// The base config must not be mutated.
	require.Equal(t, constants.DefaultOutboundThrottlerNodeMaxAtLargeBytes, int(base.NodeMaxAtLargeBytes))
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
