// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	_ AddressManager = (*addressManager)(nil)

	ErrMismatchedChainIDs = errors.New("mismatched chainIDs")
)

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
			"%w: expected %q but got %q",
			ErrMismatchedChainIDs,
			a.ctx.ChainID,
			chainID,
		)
	}
	return addr, nil
}

func (a *addressManager) ParseAddress(addrStr string) (ids.ID, ids.ShortID, error) {
	chainIDAlias, hrp, addrBytes, err := address.Parse(addrStr)
	if err != nil {
		return ids.Empty, ids.ShortID{}, err
	}

	chainID, err := a.ctx.BCLookup.Lookup(chainIDAlias)
	if err != nil {
		return ids.Empty, ids.ShortID{}, err
	}

	expectedHRP := constants.GetHRP(a.ctx.NetworkID)
	if hrp != expectedHRP {
		return ids.Empty, ids.ShortID{}, fmt.Errorf(
			"expected hrp %q but got %q",
			expectedHRP,
			hrp,
		)
	}

	addr, err := ids.ToShortID(addrBytes)
	if err != nil {
		return ids.Empty, ids.ShortID{}, err
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
	return address.Format(chainIDAlias, hrp, addr.Bytes())
}

func ParseLocalAddresses(a AddressManager, addrStrs []string) (set.Set[ids.ShortID], error) {
	addrs := make(set.Set[ids.ShortID], len(addrStrs))
	for _, addrStr := range addrStrs {
		addr, err := a.ParseLocalAddress(addrStr)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
		}
		addrs.Add(addr)
	}
	return addrs, nil
}

// ParseServiceAddress get address ID from address string, being it either localized (using address manager,
// doing also components validations), or not localized.
// If both attempts fail, reports error from localized address parsing
func ParseServiceAddress(a AddressManager, addrStr string) (ids.ShortID, error) {
	addr, err := ids.ShortFromString(addrStr)
	if err == nil {
		return addr, nil
	}

	addr, err = a.ParseLocalAddress(addrStr)
	if err != nil {
		return addr, fmt.Errorf("couldn't parse address %q: %w", addrStr, err)
	}
	return addr, nil
}

// ParseServiceAddresses get addresses IDs from addresses strings, being them either localized or not
func ParseServiceAddresses(a AddressManager, addrStrs []string) (set.Set[ids.ShortID], error) {
	addrs := set.NewSet[ids.ShortID](len(addrStrs))
	for _, addrStr := range addrStrs {
		addr, err := ParseServiceAddress(a, addrStr)
		if err != nil {
			return nil, err
		}
		addrs.Add(addr)
	}
	return addrs, nil
}
