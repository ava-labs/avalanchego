// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"github.com/ava-labs/avalanchego/ids"
)

type Versions interface {
	GetState(blkID ids.ID) (Chain, bool)
}
