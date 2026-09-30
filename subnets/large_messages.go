// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	ErrLargeMessageSizeTooSmall = fmt.Errorf("largeMessages.maxMessageSize must be greater than the default %d bytes", constants.DefaultMaxMessageSize)

	errInvalidThrottlerConfig = errors.New("invalid largeMessages.throttlerConfig")
	errThrottlerValueTooSmall = errors.New("largeMessages.throttlerConfig value must be at least maxMessageSize")
	errThrottlerValueZero     = errors.New("largeMessages.throttlerConfig value must be greater than zero")
	errRecheckDelayTooSmall   = fmt.Errorf("largeMessages.throttlerConfig recheck delay must be at least %s", constants.MinInboundThrottlerMaxRecheckDelay)
)

// LargeMessagesConfig declares that members of a subnet exchange P2P frames
// larger than [constants.DefaultMaxMessageSize].
//
// The frame size is the one value that must match between two peers, so it
// lives in the subnet config, which every node of the subnet reads from the
// same file, rather than in per-node flags. The node builds a single elevated
// stack, so at most one tracked subnet may declare this block.
//
// It can only be declared by a validator-only subnet. This ensures both ends
// of an admitted connection select the elevated stack, rather than admitting a
// public peer on one end while the other selects the larger frame size.
type LargeMessagesConfig struct {
	// MaxMessageSize is the elevated frame and codec size, in bytes.
	MaxMessageSize uint32 `json:"maxMessageSize" yaml:"maxMessageSize"`

	// ThrottlerConfig overrides individual elevated-stack throttler limits. It
	// is decoded over the limits derived from MaxMessageSize, so a key left
	// out keeps its derived value and an unknown key is rejected. It stays raw
	// because that layering needs the derived defaults, which only exist once
	// MaxMessageSize is known.
	ThrottlerConfig json.RawMessage `json:"throttlerConfig" yaml:"throttlerConfig"`
}

// LargeMessageThrottlerConfig configures the elevated stack's message
// throttlers. Its JSON keys are those of the node's own throttler
// configuration types, nested the same way.
type LargeMessageThrottlerConfig struct {
	InboundMsgThrottlerConfig  throttling.InboundMsgThrottlerConfig `json:"inboundMsgThrottlerConfig"  yaml:"inboundMsgThrottlerConfig"`
	OutboundMsgThrottlerConfig throttling.MsgByteThrottlerConfig    `json:"outboundMsgThrottlerConfig" yaml:"outboundMsgThrottlerConfig"`
}

// ResolveThrottlerConfig returns the elevated stack's throttler configuration:
// the limits derived from MaxMessageSize, with every key present in
// ThrottlerConfig decoded on top.
func (c *LargeMessagesConfig) ResolveThrottlerConfig() (LargeMessageThrottlerConfig, error) {
	throttler := DefaultLargeMessageThrottlerConfig(uint64(c.MaxMessageSize))
	if len(c.ThrottlerConfig) == 0 {
		return throttler, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(c.ThrottlerConfig))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&throttler); err != nil {
		return LargeMessageThrottlerConfig{}, fmt.Errorf("%w: %w", errInvalidThrottlerConfig, err)
	}
	return throttler, nil
}

// Verify returns nil iff this block, and the throttler configuration it
// resolves to, can be used to build the elevated stack.
func (c *LargeMessagesConfig) Verify() error {
	if c.MaxMessageSize <= constants.DefaultMaxMessageSize {
		return ErrLargeMessageSizeTooSmall
	}

	throttler, err := c.ResolveThrottlerConfig()
	if err != nil {
		return err
	}
	var (
		maxMessageSize = uint64(c.MaxMessageSize)
		inbound        = throttler.InboundMsgThrottlerConfig
		outbound       = throttler.OutboundMsgThrottlerConfig
	)
	// A peer that cannot be granted a whole frame's worth of budget stalls on
	// its first large message. The at-large pools are included because a
	// certificate-only member carries no primary network weight and so draws
	// from nothing else; VdrAllocSize is drawn second, so a small one only
	// costs a validator its head start.
	for _, limit := range []struct {
		name  string
		value uint64
	}{
		{name: "inboundMsgThrottlerConfig.byteThrottlerConfig.atLargeAllocSize", value: inbound.AtLargeAllocSize},
		{name: "inboundMsgThrottlerConfig.byteThrottlerConfig.nodeMaxAtLargeBytes", value: inbound.NodeMaxAtLargeBytes},
		{name: "inboundMsgThrottlerConfig.bandwidthThrottlerConfig.bandwidthMaxBurstRate", value: inbound.MaxBurstSize},
		{name: "outboundMsgThrottlerConfig.atLargeAllocSize", value: outbound.AtLargeAllocSize},
		{name: "outboundMsgThrottlerConfig.nodeMaxAtLargeBytes", value: outbound.NodeMaxAtLargeBytes},
	} {
		if limit.value < maxMessageSize {
			return fmt.Errorf("%w: %s is %d, want >= %d", errThrottlerValueTooSmall, limit.name, limit.value, maxMessageSize)
		}
	}

	// Zero here is a peer that can never refill or never be handled, not
	// "unlimited".
	for _, limit := range []struct {
		name  string
		value uint64
	}{
		{name: "inboundMsgThrottlerConfig.bandwidthThrottlerConfig.bandwidthRefillRate", value: inbound.RefillRate},
		{name: "inboundMsgThrottlerConfig.maxProcessingMsgsPerNode", value: inbound.MaxProcessingMsgsPerNode},
	} {
		if limit.value == 0 {
			return fmt.Errorf("%w: %s", errThrottlerValueZero, limit.name)
		}
	}

	if inbound.CPUThrottlerConfig.MaxRecheckDelay < constants.MinInboundThrottlerMaxRecheckDelay ||
		inbound.DiskThrottlerConfig.MaxRecheckDelay < constants.MinInboundThrottlerMaxRecheckDelay {
		return errRecheckDelayTooSmall
	}
	return nil
}

// DefaultLargeMessageThrottlerConfig returns a complete elevated-stack
// throttler configuration derived from [maxMessageSize]. Every byte limit
// keeps the ratio it has to the frame size under the default flags, so the
// elevated stack has the headroom the default stack has.
func DefaultLargeMessageThrottlerConfig(maxMessageSize uint64) LargeMessageThrottlerConfig {
	scale := func(defaultLimit uint64) uint64 {
		return defaultLimit * maxMessageSize / constants.DefaultMaxMessageSize
	}
	return LargeMessageThrottlerConfig{
		InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
			MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize:    scale(constants.DefaultInboundThrottlerAtLargeAllocSize),
				VdrAllocSize:        scale(constants.DefaultInboundThrottlerVdrAllocSize),
				NodeMaxAtLargeBytes: scale(constants.DefaultInboundThrottlerNodeMaxAtLargeBytes),
			},
			BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
				RefillRate:   scale(constants.DefaultInboundThrottlerBandwidthRefillRate),
				MaxBurstSize: scale(constants.DefaultInboundThrottlerBandwidthMaxBurstSize),
			},
			MaxProcessingMsgsPerNode: constants.DefaultInboundThrottlerMaxProcessingMsgsPerNode,
			CPUThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: constants.DefaultInboundThrottlerCPUMaxRecheckDelay,
			},
			DiskThrottlerConfig: throttling.SystemThrottlerConfig{
				MaxRecheckDelay: constants.DefaultInboundThrottlerDiskMaxRecheckDelay,
			},
		},
		OutboundMsgThrottlerConfig: throttling.MsgByteThrottlerConfig{
			AtLargeAllocSize:    scale(constants.DefaultOutboundThrottlerAtLargeAllocSize),
			VdrAllocSize:        scale(constants.DefaultOutboundThrottlerVdrAllocSize),
			NodeMaxAtLargeBytes: scale(constants.DefaultOutboundThrottlerNodeMaxAtLargeBytes),
		},
	}
}
