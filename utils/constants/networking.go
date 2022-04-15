// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"math"
	"time"

	"github.com/chain4travel/caminogo/utils/units"
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
