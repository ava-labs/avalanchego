// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import (
	"math"
)

// Const variables to be exported
const (
	// Request ID used when sending a Put message to gossip an accepted container
	// (ie not sent in response to a Get)
	GossipMsgRequestID = math.MaxUint32
)
