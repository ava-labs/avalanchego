// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package chains

import "github.com/ava-labs/gecko/snow/networking"

// Awaiter can await connections to be connected
type Awaiter interface {
	AwaitConnections(awaiting *networking.AwaitingConnections)
}
