// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Etna defines constants used after the Etna upgrade.
package etna

import "github.com/ava-labs/avalanchego/graft/evm/utils"

// MinBaseFee is the minimum base fee specified in ACP-125 that is allowed after
// the Etna upgrade.
//
// See: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/125-basefee-reduction
//
// This value modifies the previously used `ap4.MinBaseFee`.
const MinBaseFee = utils.GWei
