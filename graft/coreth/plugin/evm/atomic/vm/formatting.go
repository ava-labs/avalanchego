// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
)

// ParseServiceAddress get address ID from address string, being it either localized (using address manager,
// doing also components validations), or not localized.
// If both attempts fail, reports error from localized address parsing
func ParseServiceAddress(ctx *snow.Context, addrStr string) (ids.ShortID, error) {
	addr, err := ids.ShortFromString(addrStr)
	if err == nil {
		return addr, nil
	}
	return ParseLocalAddress(ctx, addrStr)
}

// ParseLocalAddress takes in an address for this chain and produces the ID
func ParseLocalAddress(ctx *snow.Context, addrStr string) (ids.ShortID, error) {
	chainID, addr, err := ParseAddress(ctx, addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf("expected chainID to be %q but was %q",
			ctx.ChainID, chainID)
	}
	return addr, nil
}

// FormatLocalAddress takes in a raw address and produces the formatted address
func FormatLocalAddress(ctx *snow.Context, addr ids.ShortID) (string, error) {
	chainIDAlias, err := ctx.BCLookup.PrimaryAlias(ctx.ChainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(ctx.NetworkID)
	return address.Format(chainIDAlias, hrp, addr.Bytes())
}

// ParseAddress takes in an address and produces the ID of the chain it's for
// the ID of the address
func ParseAddress(ctx *snow.Context, addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := address.Parse(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf("expected hrp %q but got %q",
			expectedHRP, hrp)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}
