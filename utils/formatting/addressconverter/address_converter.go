// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package addressconverter

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

// ConvertAddresses converts a list of addresses with arbitrary chains and HRPs
// (e.g. X-local1....) to a list of addresses with the provided format
// (e.g. P-custom1...).
func ConvertAddresses(destChain string, toHRP string, addresses []string) ([]string, error) {
	convertedAddrs := make([]string, len(addresses))
	for i, addr := range addresses {
		_, _, addrBytes, err := formatting.ParseAddress(addr)
		if err != nil {
			return nil, err
		}

		newAddrStr, err := formatting.FormatAddress(destChain, toHRP, addrBytes)
		if err != nil {
			return nil, err
		}
		convertedAddrs[i] = newAddrStr
	}
	return convertedAddrs, nil
}

// FormatAddressesFromID takes in a chain prefix, HRP, and slice of ids.ShortID to produce a
// slice of strings for the given addresses.
func FormatAddressesFromID(
	chainIDAlias string,
	hrp string,
	addrs []ids.ShortID,
) ([]string, error) {
	var err error
	addrsStr := make([]string, len(addrs))
	for i, addr := range addrs {
		addrsStr[i], err = formatting.FormatAddress(chainIDAlias, hrp, addr[:])
		if err != nil {
			return nil, fmt.Errorf("could not format address %s, chain %s, hrp %s: %w", addr, chainIDAlias, hrp, err)
		}
	}
	return addrsStr, nil
}

func ParseAddressToID(addrStr string) (ids.ShortID, error) {
	_, _, addrBytes, err := formatting.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	return ids.ToShortID(addrBytes)
}

func ParseAddressesToID(addrsStr []string) ([]ids.ShortID, error) {
	var err error
	addrs := make([]ids.ShortID, len(addrsStr))
	for i, addrStr := range addrsStr {
		addrs[i], err = ParseAddressToID(addrStr)
		if err != nil {
			return nil, err
		}
	}
	return addrs, nil
}
