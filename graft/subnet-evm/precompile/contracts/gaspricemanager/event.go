// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gaspricemanager

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
)

// GasPriceConfigUpdatedEventData holds the non-indexed data from a GasPriceConfigUpdated event.
type GasPriceConfigUpdatedEventData struct {
	OldGasPriceConfig commontype.GasPriceConfig
	NewGasPriceConfig commontype.GasPriceConfig
}

// PackGasPriceConfigUpdatedEvent returns topic hashes and ABI-encoded non-indexed data.
func PackGasPriceConfigUpdatedEvent(sender common.Address, oldConfig commontype.GasPriceConfig, newConfig commontype.GasPriceConfig) ([]common.Hash, []byte, error) {
	return GasPriceManagerABI.PackEvent("GasPriceConfigUpdated", sender, oldConfig, newConfig)
}

// UnpackGasPriceConfigUpdatedEventData decodes the non-indexed portion of a GasPriceConfigUpdated log.
func UnpackGasPriceConfigUpdatedEventData(dataBytes []byte) (GasPriceConfigUpdatedEventData, error) {
	var data GasPriceConfigUpdatedEventData
	err := GasPriceManagerABI.UnpackIntoInterface(&data, "GasPriceConfigUpdated", dataBytes)
	return data, err
}
