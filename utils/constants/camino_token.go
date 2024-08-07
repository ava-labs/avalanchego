// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package constants

// Const variables to be exported
const (
	DefaultTokenSymbl = "AVAX"
	CaminoTokenSymbol = "CAM"

	DefaultTokenName = "Avalanche"
	CaminoTokenName  = "Camino"
)

// Variables to be exported
var (
	NetworkIDToTokenSymbol = map[uint32]string{
		CaminoID:     CaminoTokenSymbol,
		ColumbusID:   CaminoTokenSymbol,
		KopernikusID: CaminoTokenSymbol,
	}

	NetworkIDToTokenName = map[uint32]string{
		CaminoID:     CaminoTokenName,
		ColumbusID:   CaminoTokenName,
		KopernikusID: CaminoTokenName,
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
