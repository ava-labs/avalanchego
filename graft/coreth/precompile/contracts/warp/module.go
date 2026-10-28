// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import "github.com/ava-labs/libevm/common"

// ConfigKey is the key used in json config files to specify this precompile config.
// must be unique across all precompiles.
const ConfigKey = "warpConfig"

// ContractAddress is the address of the warp precompile contract
var ContractAddress = common.HexToAddress("0x0200000000000000000000000000000000000005")
