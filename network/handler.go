// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import "github.com/ava-labs/gecko/ids"

// Handler represents a handler that is called when a connection is marked as
// connected or disconnected
type Handler interface {
	// returns true if the handler should be removed
	Connected(id ids.ShortID) bool
	Disconnected(id ids.ShortID) bool
}
