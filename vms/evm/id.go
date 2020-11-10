// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"github.com/ava-labs/avalanchego/ids"
)

var (
	// ID that this VM uses when labeled
	ID = ids.NewID([32]byte{'e', 'v', 'm'})
)
