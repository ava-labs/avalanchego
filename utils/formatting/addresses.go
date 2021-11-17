// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"errors"
	"fmt"
	"strings"

	"github.com/btcsuite/btcutil/bech32"
)

const (
	addressSep = "-"
)

var (
	errNoSeparator = errors.New("no separator found in address")
	errBits5To8    = errors.New("unable to convert address from 5-bit to 8-bit formatting")
	errBits8To5    = errors.New("unable to convert address from 8-bit to 5-bit formatting")
)

// ParseAddress takes in an address string and splits returns the corresponding
// parts. This returns the chain ID alias, bech32 HRP, address bytes, and an
// error if it occurs.
func ParseAddress(addrStr string) (string, string, []byte, error) {
	addressParts := strings.SplitN(addrStr, addressSep, 2)
	if len(addressParts) < 2 {
		return "", "", nil, errNoSeparator
	}
	chainID := addressParts[0]
	rawAddr := addressParts[1]

	hrp, addr, err := ParseBech32(rawAddr)
	return chainID, hrp, addr, err
}

// FormatAddress takes in a chain prefix, HRP, and byte slice to produce a
// string for an address.
func FormatAddress(
	chainIDAlias string,
	hrp string,
	addr []byte,
) (string, error) {
	addrStr, err := FormatBech32(hrp, addr)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", chainIDAlias, addressSep, addrStr), nil
}

// ParseBech32 takes a bech32 address as input and returns the HRP and data
// section of a bech32 address
func ParseBech32(addrStr string) (string, []byte, error) {
	rawHRP, decoded, err := bech32.Decode(addrStr)
	if err != nil {
		return "", nil, err
	}
	addrBytes, err := bech32.ConvertBits(decoded, 5, 8, true)
	if err != nil {
		return "", nil, errBits5To8
	}
	return rawHRP, addrBytes, nil
}

// FormatBech32 takes an address's bytes as input and returns a bech32 address
func FormatBech32(hrp string, payload []byte) (string, error) {
	fiveBits, err := bech32.ConvertBits(payload, 8, 5, true)
	if err != nil {
		return "", errBits8To5
	}
	return bech32.Encode(hrp, fiveBits)
}
