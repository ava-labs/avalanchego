// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/utils/constants"
)

const (
	// The elevated stack's throttler limits are derived from its frame size
	// with these factors, which keep the headroom the default 2 MiB stack has
	// under the default throttler flags; each trailing comment names it.
	inboundAtLargeAllocMultiplier     = 3  // 6 MiB at-large
	outboundAtLargeAllocMultiplier    = 16 // 32 MiB at-large
	validatorAllocMultiplier          = 16 // 32 MiB validator
	inboundBandwidthRefillRateDivisor = 4  // 512 KiB/s refill
)

var (
	ErrLargeMessageSizeTooSmall = fmt.Errorf("largeMessages.maxMessageSize must be greater than the default %d bytes", constants.DefaultMaxMessageSize)

	errThrottlerValueTooSmall = errors.New("largeMessages.throttlerConfig value must be at least maxMessageSize")
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

	// ThrottlerConfig overrides individual elevated-stack throttler limits.
	// Every field left at zero is derived from MaxMessageSize.
	ThrottlerConfig *LargeMessageThrottlerConfig `json:"throttlerConfig" yaml:"throttlerConfig"`
}

// LargeMessageThrottlerConfig configures the elevated stack's message
// throttlers. A zero field means "derive from the max message size"; none of
// these limits is meaningfully zero.
type LargeMessageThrottlerConfig struct {
	InboundMsgThrottlerConfig  throttling.InboundMsgThrottlerConfig `json:"inboundMsgThrottlerConfig"  yaml:"inboundMsgThrottlerConfig"`
	OutboundMsgThrottlerConfig throttling.MsgByteThrottlerConfig    `json:"outboundMsgThrottlerConfig" yaml:"outboundMsgThrottlerConfig"`
}

// Throttler returns the elevated stack's throttler configuration: the limits
// derived from MaxMessageSize, with every non-zero override applied on top.
func (c *LargeMessagesConfig) Throttler() LargeMessageThrottlerConfig {
	throttler := DefaultLargeMessageThrottlerConfig(uint64(c.MaxMessageSize))
	if c.ThrottlerConfig == nil {
		return throttler
	}

	var (
		inbound   = &throttler.InboundMsgThrottlerConfig
		outbound  = &throttler.OutboundMsgThrottlerConfig
		oInbound  = c.ThrottlerConfig.InboundMsgThrottlerConfig
		oOutbound = c.ThrottlerConfig.OutboundMsgThrottlerConfig
	)
	override(&inbound.AtLargeAllocSize, oInbound.AtLargeAllocSize)
	override(&inbound.VdrAllocSize, oInbound.VdrAllocSize)
	override(&inbound.NodeMaxAtLargeBytes, oInbound.NodeMaxAtLargeBytes)
	override(&inbound.MaxProcessingMsgsPerNode, oInbound.MaxProcessingMsgsPerNode)
	override(&inbound.RefillRate, oInbound.RefillRate)
	override(&inbound.MaxBurstSize, oInbound.MaxBurstSize)
	override(&inbound.CPUThrottlerConfig.MaxRecheckDelay, oInbound.CPUThrottlerConfig.MaxRecheckDelay)
	override(&inbound.DiskThrottlerConfig.MaxRecheckDelay, oInbound.DiskThrottlerConfig.MaxRecheckDelay)
	override(&outbound.AtLargeAllocSize, oOutbound.AtLargeAllocSize)
	override(&outbound.VdrAllocSize, oOutbound.VdrAllocSize)
	override(&outbound.NodeMaxAtLargeBytes, oOutbound.NodeMaxAtLargeBytes)
	return throttler
}

// Verify returns nil iff this block, and the throttler configuration it
// resolves to, can be used to build the elevated stack.
func (c *LargeMessagesConfig) Verify() error {
	if c.MaxMessageSize <= constants.DefaultMaxMessageSize {
		return ErrLargeMessageSizeTooSmall
	}

	var (
		maxMessageSize = uint64(c.MaxMessageSize)
		throttler      = c.Throttler()
		inbound        = throttler.InboundMsgThrottlerConfig
		outbound       = throttler.OutboundMsgThrottlerConfig
	)
	// A peer that cannot be granted a whole frame's worth of budget would stall
	// on the first large message.
	for _, limit := range []struct {
		name  string
		value uint64
	}{
		{name: "inboundMsgThrottlerConfig.nodeMaxAtLargeBytes", value: inbound.NodeMaxAtLargeBytes},
		{name: "inboundMsgThrottlerConfig.maxBurstSize", value: inbound.MaxBurstSize},
		{name: "outboundMsgThrottlerConfig.nodeMaxAtLargeBytes", value: outbound.NodeMaxAtLargeBytes},
	} {
		if limit.value < maxMessageSize {
			return fmt.Errorf("%w: %s is %d, want >= %d", errThrottlerValueTooSmall, limit.name, limit.value, maxMessageSize)
		}
	}

	if inbound.CPUThrottlerConfig.MaxRecheckDelay < constants.MinInboundThrottlerMaxRecheckDelay ||
		inbound.DiskThrottlerConfig.MaxRecheckDelay < constants.MinInboundThrottlerMaxRecheckDelay {
		return errRecheckDelayTooSmall
	}
	return nil
}

// DefaultLargeMessageThrottlerConfig returns a complete elevated-stack
// throttler configuration derived from [maxMessageSize]. The multipliers keep
// the same headroom relative to the frame size that the default 2 MiB stack
// has.
func DefaultLargeMessageThrottlerConfig(maxMessageSize uint64) LargeMessageThrottlerConfig {
	return LargeMessageThrottlerConfig{
		InboundMsgThrottlerConfig: throttling.InboundMsgThrottlerConfig{
			MsgByteThrottlerConfig: throttling.MsgByteThrottlerConfig{
				AtLargeAllocSize:    maxMessageSize * inboundAtLargeAllocMultiplier,
				VdrAllocSize:        maxMessageSize * validatorAllocMultiplier,
				NodeMaxAtLargeBytes: maxMessageSize,
			},
			BandwidthThrottlerConfig: throttling.BandwidthThrottlerConfig{
				RefillRate:   maxMessageSize / inboundBandwidthRefillRateDivisor,
				MaxBurstSize: maxMessageSize,
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
			AtLargeAllocSize:    maxMessageSize * outboundAtLargeAllocMultiplier,
			VdrAllocSize:        maxMessageSize * validatorAllocMultiplier,
			NodeMaxAtLargeBytes: maxMessageSize,
		},
	}
}

// override leaves *target at its derived default when [value] is unset.
func override[T comparable](target *T, value T) {
	var zero T
	if value != zero {
		*target = value
	}
}
