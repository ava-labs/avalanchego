// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

// ConvertAddresses converts a list of addresses with arbitrary chains and HRPs
// (e.g. X-local1....) to a list of addresses with the provided format
// (e.g. P-custom1...).
func ConvertAddresses(destChain string, toHRP string, addresses []string) ([]string, error) {
	convertedAddrs := make([]string, len(addresses))
	for i, addr := range addresses {
		_, _, addrBytes, err := ParseAddress(addr)
		if err != nil {
			return nil, err
		}

		newAddrStr, err := FormatAddress(destChain, toHRP, addrBytes)
		if err != nil {
			return nil, err
		}
		convertedAddrs[i] = newAddrStr
	}
	return convertedAddrs, nil
}
