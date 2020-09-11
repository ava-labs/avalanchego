// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/utils"
)

// Peer contains the specification of an Avalanche node that can be communicated with.
type Peer struct {
	// IP of the peer
	IP utils.IPDesc
	// ID of the peer that can be verified during a handshake
	ID ids.ShortID
}
