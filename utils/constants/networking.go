// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"math"

	"github.com/ava-labs/avalanchego/utils/units"
)

// Const variables to be exported
const (
	// Request ID used when sending a Put message to gossip an accepted container
	// (ie not sent in response to a Get)
	GossipMsgRequestID = math.MaxUint32

	// The network must be "tcp", "tcp4", "tcp6", "unix" or "unixpacket".
	NetworkType = "tcp"

	DefaultMaxMessageSize uint32 = 2 * units.MiB

	MaxContainersLen = int(4 * DefaultMaxMessageSize / 5)
)
