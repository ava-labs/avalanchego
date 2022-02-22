// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package params

import "github.com/ethereum/go-ethereum/common"

// Gas costs for stateful precompiles
const (
	ModifyAllowListGasCost = 20_000
)

// Designated addresses of stateful precompiles
var (
	ModifyAllowListAddress = common.HexToAddress("0x0200000000000000000000000000000000000000")
)
