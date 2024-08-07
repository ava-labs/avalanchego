// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package treasury

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	// Addr is treasury address
	Addr = ids.ShortID{
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x0c,
	}
	// 0x010000000000000000000000000000000000000c
	// P-kopernikus1qyqqqqqqqqqqqqqqqqqqqqqqqqqqqqqvy7p25h
	// 6HgC8KRBEhXYbF4riJyJFLSHt4U9qQDq

	AddrTraitsBytes = [][]byte{Addr[:]}

	Owner = &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{Addr},
	}
)
