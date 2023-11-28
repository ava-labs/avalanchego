// (c) 2021-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package utils

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
)

// AddressRange represents a continuous range of addresses
type AddressRange struct {
	Start common.Address
	End   common.Address
}

// Contains returns true iff [addr] is contained within the (inclusive)
// range of addresses defined by [a].
func (a *AddressRange) Contains(addr common.Address) bool {
	addrBytes := addr.Bytes()
	return bytes.Compare(addrBytes, a.Start[:]) >= 0 && bytes.Compare(addrBytes, a.End[:]) <= 0
}
