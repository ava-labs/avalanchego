// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

// Const variables to be exported
const (
	DefaultTokenSymbl   = "AVAX"
	ColumbusTokenSymbol = "CAM"

	DefaultTokenName  = "Avalanche"
	ColumbusTokenName = "Camino"
)

// Variables to be exported
var (
	NetworkIDToTokenSymbol = map[uint32]string{
		ColumbusID: ColumbusTokenSymbol,
	}

	NetworkIDToTokenName = map[uint32]string{
		ColumbusID: ColumbusTokenName,
	}
)

// GetHRP returns the Human-Readable-Part of bech32 addresses for a networkID
func TokenSymbol(networkID uint32) string {
	if symbol, ok := NetworkIDToTokenSymbol[networkID]; ok {
		return symbol
	}
	return DefaultTokenSymbl
}

// NetworkName returns a human readable name for the network with
// ID [networkID]
func TokenName(networkID uint32) string {
	if name, ok := NetworkIDToTokenName[networkID]; ok {
		return name
	}
	return DefaultTokenName
}
