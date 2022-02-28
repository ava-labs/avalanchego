// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package precompile

import "github.com/ethereum/go-ethereum/common"

// Gas costs for stateful precompiles
const (
	ModifyAllowListGasCost = 20_000
	ReadAllowListGasCost   = 5_000
)

// Designated addresses of stateful precompiles
// Note: it is important that none of these addresses conflict with each other or any other precompiles
// in core/vm/contracts.go.
// We start at 0x0200000000000000000000000000000000000000 and will increment by 1 from here to reduce
// the risk of conflicts.
// For forks of subnet-evm, users should start at 0x0300000000000000000000000000000000000000 to ensure
// that their own modifications do not conflict with stateful precompiles that may be added to subnet-evm
// in the future.
var (
	AllowListAddress = common.HexToAddress("0x0200000000000000000000000000000000000000")

	PrecompileAddresses = []common.Address{
		AllowListAddress,
	}
)
