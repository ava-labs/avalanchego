// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"math"
	"time"

	"github.com/ava-labs/avalanchego/utils/units"
)

// Const variables to be exported
const (
	// Request ID used when sending a Put message to gossip an accepted container
	// (ie not sent in response to a Get)
	GossipMsgRequestID uint32 = math.MaxUint32

	// The network must be "tcp", "tcp4", "tcp6", "unix" or "unixpacket".
	NetworkType = "tcp"

	DefaultMaxMessageSize  = 2 * units.MiB
	DefaultPingPongTimeout = 30 * time.Second
	DefaultPingFrequency   = 3 * DefaultPingPongTimeout / 4
	DefaultByteSliceCap    = 128

	MaxContainersLen = int(4 * DefaultMaxMessageSize / 5)
)
