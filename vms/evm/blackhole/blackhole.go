// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package blackhole defines the address to which EVM chains burn fees.
package blackhole

import "github.com/ava-labs/libevm/common"

// Address is the coinbase that receives burned fees.
var Address = common.HexToAddress("0x0100000000000000000000000000000000000000")
