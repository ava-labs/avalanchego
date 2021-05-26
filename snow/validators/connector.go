// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validators

import (
	"github.com/ava-labs/avalanchego/ids"
)

// Connector represents a handler that is called when a connection is marked as
// connected or disconnected
type Connector interface {
	Connected(id ids.ShortID) error
	Disconnected(id ids.ShortID) error
}
