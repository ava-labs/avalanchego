// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package address

import (
	"github.com/ava-labs/avalanchego/ids"
)

// ConvertAddresses converts a list of addresses with arbitrary chains and HRPs
// (e.g. X-local1....) to a list of addresses with the provided format
// (e.g. P-custom1...).
func ConvertAddresses(destChain string, toHRP string, addresses []string) ([]string, error) {
	convertedAddrs := make([]string, len(addresses))
	for i, addr := range addresses {
		_, _, addrBytes, err := Parse(addr)
		if err != nil {
			return nil, err
		}

		newAddrStr, err := Format(destChain, toHRP, addrBytes)
		if err != nil {
			return nil, err
		}
		convertedAddrs[i] = newAddrStr
	}
	return convertedAddrs, nil
}

func ParseToID(addrStr string) (ids.ShortID, error) {
	_, _, addrBytes, err := Parse(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	return ids.ToShortID(addrBytes)
}

func ParseToIDs(addrStrs []string) ([]ids.ShortID, error) {
	var err error
	addrs := make([]ids.ShortID, len(addrStrs))
	for i, addrStr := range addrStrs {
		addrs[i], err = ParseToID(addrStr)
		if err != nil {
			return nil, err
		}
	}
	return addrs, nil
}
