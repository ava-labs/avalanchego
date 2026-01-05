// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package address

import "github.com/ava-labs/avalanchego/ids"

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
