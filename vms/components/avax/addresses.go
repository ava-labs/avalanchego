// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting"
)

var _ AddressManager = &addressManager{}

type AddressManager interface {
	// ParseLocalAddress takes in an address for this chain and produces the ID
	ParseLocalAddress(addrStr string) (ids.ShortID, error)

	// ParseAddress takes in an address and produces the ID of the chain it's
	// for and the ID of the address
	ParseAddress(addrStr string) (ids.ID, ids.ShortID, error)

	// FormatLocalAddress takes in a raw address and produces the formatted
	// address for this chain
	FormatLocalAddress(addr ids.ShortID) (string, error)

	// FormatAddress takes in a chainID and a raw address and produces the
	// formatted address for that chain
	FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error)
}

type addressManager struct {
	ctx *snow.Context
}

func NewAddressManager(ctx *snow.Context) AddressManager {
	return &addressManager{
		ctx: ctx,
	}
}

func (a *addressManager) ParseLocalAddress(addrStr string) (ids.ShortID, error) {
	chainID, addr, err := a.ParseAddress(addrStr)
	if err != nil {
		return ids.ShortID{}, err
	}
	if chainID != a.ctx.ChainID {
		return ids.ShortID{}, fmt.Errorf(
			"expected chainID to be %q but was %q",
			a.ctx.ChainID,
			chainID,
		)
	}
	return addr, nil
}

func (a *addressManager) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := formatting.ParseAddress(addrStr)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	chainID, err := a.ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(a.ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.ID{}, ids.ShortID{}, fmt.Errorf(
			"expected hrp %q but got %q",
			expectedHRP,
			hrp,
		)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.ID{}, ids.ShortID{}, err
	}
	return chainID, addr, nil
}

func (a *addressManager) FormatLocalAddress(addr ids.ShortID) (string, error) {
	return a.FormatAddress(a.ctx.ChainID, addr)
}

func (a *addressManager) FormatAddress(chainID ids.ID, addr ids.ShortID) (string, error) {
	chainIDAlias, err := a.ctx.BCLookup.PrimaryAlias(chainID)
	if err != nil {
		return "", err
	}
	hrp := constants.GetHRP(a.ctx.NetworkID)
	return formatting.FormatAddress(chainIDAlias, hrp, addr.Bytes())
}

func ParseLocalAddresses(a AddressManager, addrStrs []string) (ids.ShortSet, error) {
	addrs := make(ids.ShortSet, len(addrStrs))
	for _, addrStr := range addrStrs {
		addr, err := a.ParseLocalAddress(addrStr)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrs.Add(addr)
	}
	return addrs, nil
}
