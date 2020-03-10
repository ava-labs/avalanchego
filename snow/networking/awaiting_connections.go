// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"github.com/ava-labs/gecko/ids"
)

// AwaitingConnections ...
type AwaitingConnections struct {
	Requested   ids.ShortSet
	NumRequired int
	Finish      func()

	connected ids.ShortSet
}

// Add ...
func (aw *AwaitingConnections) Add(conn ids.ShortID) {
	if aw.Requested.Contains(conn) {
		aw.connected.Add(conn)
	}
}

// Remove ...
func (aw *AwaitingConnections) Remove(conn ids.ShortID) {
	aw.connected.Remove(conn)
}

// Ready ...
func (aw *AwaitingConnections) Ready() bool {
	return aw.connected.Len() >= aw.NumRequired
}
