// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

const testElevatedMessageSize = 4 * constants.DefaultMaxMessageSize

// newMembershipTestNetwork returns a network with just the pieces that decide
// which stack a peer is put on: the config, the message stacks, the membership
// and the ip tracker.
func newMembershipTestNetwork(t *testing.T, configure func(*Config)) *network {
	t.Helper()

	cfg, err := NewTestNetworkConfig(
		prometheus.NewRegistry(),
		constants.LocalID,
		validators.NewManager(),
		set.Set[ids.ID]{},
	)
	require.NoError(t, err)
	if configure != nil {
		configure(cfg)
	}
	// Node config produces exactly this relationship: a config per tracked
	// subnet, plus the primary network.
	cfg.TrackedSubnets = trackedSubnetsOf(cfg.SubnetConfigs)

	stacks, err := newMessageStacks(
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.Validators,
		cfg,
	)
	require.NoError(t, err)

	ipTracker, err := newIPTracker(
		cfg.TrackedSubnets,
		logging.NoLog{},
		prometheus.NewRegistry(),
		cfg.ConnectToAllValidators,
	)
	require.NoError(t, err)

	return &network{
		config:        cfg,
		messageStacks: stacks,
		membership:    newMembership(cfg.SubnetConfigs, cfg.TrackedSubnets, cfg.Validators),
		ipTracker:     ipTracker,
	}
}

func elevatedSubnetConfig(allowedNodes ...ids.NodeID) subnets.Config {
	return subnets.Config{
		ValidatorOnly: true,
		AllowedNodes:  set.Of(allowedNodes...),
		LargeMessages: &subnets.LargeMessagesConfig{
			MaxMessageSize: testElevatedMessageSize,
		},
	}
}

func withElevatedSubnet(subnetID ids.ID, subnetConfig subnets.Config) func(*Config) {
	return func(cfg *Config) {
		cfg.SubnetConfigs = map[ids.ID]subnets.Config{subnetID: subnetConfig}
	}
}

// TestStackForMembership checks that the elevated stack follows membership of
// the subnet that declares largeMessages, by each of the three member sources.
func TestStackForMembership(t *testing.T) {
	var (
		subnetID  = ids.GenerateTestID()
		validator = ids.GenerateTestNodeID()
		allowed   = ids.GenerateTestNodeID()
		certPeer  = ids.GenerateTestNodeID()
		stranger  = ids.GenerateTestNodeID()
	)

	n := newMembershipTestNetwork(t, withElevatedSubnet(subnetID, elevatedSubnetConfig(allowed)))
	require.NoError(t, n.config.Validators.AddStaker(subnetID, validator, nil, ids.GenerateTestID(), 1))
	n.membership.track(certPeer, certifiedSubnets{
		subnetID: time.Now().Add(time.Hour),
	})

	for name, test := range map[string]struct {
		nodeID   ids.NodeID
		elevated bool
	}{
		"subnet validator":   {nodeID: validator, elevated: true},
		"certificate member": {nodeID: certPeer, elevated: true},
		"allowedNodes entry": {nodeID: allowed, elevated: true},
		"stranger":           {nodeID: stranger, elevated: false},
	} {
		t.Run(name, func(t *testing.T) {
			want := n.messageStacks.Default
			if test.elevated {
				want = n.messageStacks.elevated.messageStack
			}
			require.Equal(t, want, n.stackFor(test.nodeID))
			require.Equal(t, want.MaxFrameSize, n.FrameSize(test.nodeID))
		})
	}

	// The two stacks must be distinct, otherwise an elevated peer would inherit
	// the default frame limit.
	require.NotEqual(t, n.messageStacks.Default.MessageCreator, n.messageStacks.elevated.messageStack.MessageCreator)
	require.NotEqual(t, n.messageStacks.Default.MaxFrameSize, n.messageStacks.elevated.messageStack.MaxFrameSize)
}

// TestStackForReadsTrackedMembership checks that stackFor reads the certificate
// membership [membership.track] recorded, which is why the connection path
// tracks the peer before it selects a stack.
func TestStackForReadsTrackedMembership(t *testing.T) {
	var (
		subnetID = ids.GenerateTestID()
		nodeID   = ids.GenerateTestNodeID()
	)

	n := newMembershipTestNetwork(t, withElevatedSubnet(subnetID, elevatedSubnetConfig()))

	require.Equal(t, n.messageStacks.Default, n.stackFor(nodeID))

	n.membership.track(nodeID, certifiedSubnets{
		subnetID: time.Now().Add(time.Hour),
	})
	require.Equal(t, n.messageStacks.elevated.messageStack, n.stackFor(nodeID))
}

func TestStackForDisabled(t *testing.T) {
	n := newMembershipTestNetwork(t, nil)

	require.False(t, n.messageStacks.elevated.hasElevated)
	require.Equal(t, n.messageStacks.Default, n.stackFor(ids.GenerateTestNodeID()))
	require.Equal(t, uint32(constants.DefaultMaxMessageSize), n.FrameSize(ids.GenerateTestNodeID()))
}

// TestLargeMessagesSubnet checks that the node's single elevated stack is
// resolved from the one subnet that declares largeMessages.
func TestLargeMessagesSubnet(t *testing.T) {
	var (
		subnetID      = ids.GenerateTestID()
		otherSubnetID = ids.GenerateTestID()
		largeMessages = &subnets.LargeMessagesConfig{
			MaxMessageSize: testElevatedMessageSize,
		}
	)

	t.Run("no subnet declares largeMessages", func(t *testing.T) {
		require := require.New(t)

		elevatedSubnetID, got, err := largeMessagesSubnet(map[ids.ID]subnets.Config{
			constants.PrimaryNetworkID: {},
			subnetID:                   {ValidatorOnly: true},
		})
		require.NoError(err)
		require.Nil(got)
		require.Equal(ids.Empty, elevatedSubnetID)
	})

	t.Run("one subnet declares largeMessages", func(t *testing.T) {
		require := require.New(t)

		elevatedSubnetID, got, err := largeMessagesSubnet(map[ids.ID]subnets.Config{
			constants.PrimaryNetworkID: {},
			subnetID: {
				ValidatorOnly: true,
				LargeMessages: largeMessages,
			},
		})
		require.NoError(err)
		require.Equal(subnetID, elevatedSubnetID)
		require.Equal(largeMessages, got)
	})

	t.Run("two subnets declare largeMessages", func(t *testing.T) {
		_, _, err := largeMessagesSubnet(map[ids.ID]subnets.Config{
			constants.PrimaryNetworkID: {},
			subnetID:                   {LargeMessages: largeMessages},
			otherSubnetID:              {LargeMessages: largeMessages},
		})
		require.ErrorIs(t, err, errTooManyLargeMessageSubnets)
	})
}

// TestLargeMessagesLogged checks that a node running the elevated stack says so
// at startup, which is the cheapest way to catch a node missed in a rollout.
func TestLargeMessagesLogged(t *testing.T) {
	require := require.New(t)

	subnetID := ids.GenerateTestID()
	config, err := NewTestNetworkConfig(
		prometheus.NewRegistry(),
		constants.LocalID,
		validators.NewManager(),
		set.Of(subnetID),
	)
	require.NoError(err)
	config.SubnetConfigs = map[ids.ID]subnets.Config{
		subnetID: elevatedSubnetConfig(),
	}

	core, logs := observer.New(zapcore.Level(logging.Warn))
	_, err = newMessageStacks(
		logging.NewLogger("", logging.WrappedCore{Core: core}),
		prometheus.NewRegistry(),
		config.Validators,
		config,
	)
	require.NoError(err)

	entries := logs.All()
	require.Len(entries, 1)
	require.Equal("large message config enabled", entries[0].Message)
	require.Equal(uint32(testElevatedMessageSize), entries[0].ContextMap()["maxMessageSize"])
}

// TestMsgCreator checks that the node-wide creator is the elevated one whenever
// that stack exists, so the sender can build payloads above the default size at
// all.
func TestMsgCreator(t *testing.T) {
	t.Run("elevated stack", func(t *testing.T) {
		n := newMembershipTestNetwork(t, withElevatedSubnet(ids.GenerateTestID(), elevatedSubnetConfig()))

		require.Equal(t, n.messageStacks.elevated.messageStack.MessageCreator, n.MsgCreator())
		require.NotEqual(t, n.messageStacks.Default.MessageCreator, n.MsgCreator())
	})

	t.Run("no elevated stack", func(t *testing.T) {
		n := newMembershipTestNetwork(t, nil)

		require.Equal(t, n.messageStacks.Default.MessageCreator, n.MsgCreator())
	})
}
