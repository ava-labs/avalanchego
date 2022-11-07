// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/lock"
)

// For state and diff
type Camino interface {
	LockedUTXOs(ids.Set, ids.ShortSet, lock.LockState) ([]*avax.UTXO, error)
}
