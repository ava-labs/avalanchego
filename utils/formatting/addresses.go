// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package formatting

import (
	"fmt"
	"strings"

	"github.com/btcsuite/btcutil/bech32"
)

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// ParseBech32 takes a bech32 address as input and returns the HRP and data section of a bech32 address
func ParseBech32(addrStr string) (string, []byte, error) {
	rawHRP, decoded, err := bech32.Decode(addrStr)
	if err != nil {
		return "", nil, err
	}
	addrbuff, err := bech32.ConvertBits(decoded, 5, 8, true)
	if err != nil {
		return "", nil, fmt.Errorf("unable to convert address from 5-bit to 8-bit formatting")
	}
	return rawHRP, addrbuff, nil
}

// FormatBech32 takes an address's bytes as input and returns a bech32 address
func FormatBech32(hrp string, b []byte) (string, error) {
	fivebits, err := bech32.ConvertBits(b, 8, 5, true)
	if err != nil {
		return "", fmt.Errorf("unable to convert address from 8-bit to 5-bit formatting")
	}
	addr, err := bech32.Encode(hrp, fivebits)
	if err != nil {
		return "", err
	}
	return addr, nil
}

// ParseAddress takes in an address string, a chain prefix array, a separator, and an HRP, validates against a chain prefix, and
//    an HRP, to produce bytes for the address
func ParseAddress(addrStr string, chainPrefixes []string, addressSep string, hrp string) ([]byte, error) {

	if count := strings.Count(addrStr, addressSep); count < 1 {
		return nil, fmt.Errorf("address is missing a chainID")
	}
	addressParts := strings.SplitN(addrStr, addressSep, 2)
	bcID := addressParts[0]
	rawAddr := addressParts[1]

	if !stringInSlice(bcID, chainPrefixes) {
		return nil, fmt.Errorf("invalid chainID in address, needed %v", chainPrefixes)
	}
	_, addr, err := ParseBech32(rawAddr)
	if err != nil {
		return nil, err
	}
	return addr, nil
}

// FormatAddress takes in a 20-byte slice, a chain prefix, a separator, and an HRP, to produce a string for an address
func FormatAddress(b []byte, chainPrefix string, addressSep string, hrp string) (string, error) {
	addrstr, err := FormatBech32(hrp, b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s%s", chainPrefix, addressSep, addrstr), nil
}
