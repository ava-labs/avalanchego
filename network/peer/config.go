// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
	"github.com/ava-labs/avalanchego/network/throttling"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/networking/tracker"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/version"
)

type Config struct {
	// Size, in bytes, of the buffer this peer reads messages into
	ReadBufferSize int
	// Size, in bytes, of the buffer this peer writes messages into
	WriteBufferSize         int
	Clock                   mockable.Clock
	Metrics                 *Metrics
	MessageCreator          message.Creator
	MessageCreatorWithProto message.Creator

	// TODO: remove this once we complete banff migration
	BanffTime time.Time

	Log                  logging.Logger
	InboundMsgThrottler  throttling.InboundMsgThrottler
	Network              Network
	Router               router.InboundHandler
	VersionCompatibility version.Compatibility
	MySubnets            ids.Set
	Beacons              validators.Set
	NetworkID            uint32
	PingFrequency        time.Duration
	PongTimeout          time.Duration
	MaxClockDifference   time.Duration

	// Unix time of the last message sent and received respectively
	// Must only be accessed atomically
	LastSent, LastReceived int64

	// Tracks CPU/disk usage caused by each peer.
	ResourceTracker tracker.ResourceTracker
}

func (c *Config) GetMessageCreator() message.Creator {
	now := c.Clock.Time()
	if c.IsBanffActivated(now) {
		return c.MessageCreatorWithProto
	}
	return c.MessageCreator
}

func (c *Config) IsBanffActivated(time time.Time) bool {
	return !time.Before(c.BanffTime)
}
