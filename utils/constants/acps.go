// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/ava-labs/avalanchego/utils/set"

// CurrentACPs is the set of ACPs that are currently, at the time of release,
// marked as implementable.
//
// See: https://github.com/avalanche-foundation/ACPs/tree/main#readme
var CurrentACPs = set.Of[uint32](
	23, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/23-p-chain-native-transfers.md
	24, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/24-shanghai-eips.md
	25, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/25-vm-application-errors.md
	30, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/30-avalanche-warp-x-evm.md
	31, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/31-enable-subnet-ownership-transfer.md
	41, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/41-remove-pending-stakers.md
)
