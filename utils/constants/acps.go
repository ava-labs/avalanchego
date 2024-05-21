// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

import "github.com/ava-labs/avalanchego/utils/set"

var (
	// ActivatedACPs is the set of ACPs that are activated.
	//
	// See: https://github.com/orgs/avalanche-foundation/projects/1
	ActivatedACPs = set.Of[uint32](
		23, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/23-p-chain-native-transfers/README.md
		24, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/24-shanghai-eips/README.md
		25, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/25-vm-application-errors/README.md
		30, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/30-avalanche-warp-x-evm/README.md
		31, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/31-enable-subnet-ownership-transfer/README.md
		41, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/41-remove-pending-stakers/README.md
		62, // https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/62-disable-addvalidatortx-and-adddelegatortx/README.md
	)

	// CurrentACPs is the set of ACPs that are currently, at the time of
	// release, marked as implementable and not activated.
	//
	// See: https://github.com/orgs/avalanche-foundation/projects/1
	CurrentACPs = set.Of[uint32]()

	// ScheduledACPs are the ACPs included into the next upgrade.
	ScheduledACPs = set.Of[uint32]()
)
