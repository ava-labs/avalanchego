// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
)

// FeeConfigUpdatedEventData holds the non-indexed data from a FeeConfigUpdated event.
type FeeConfigUpdatedEventData struct {
	OldFeeConfig commontype.ACP224FeeConfig
	NewFeeConfig commontype.ACP224FeeConfig
}

type abiFeeConfigUpdatedEventData struct {
	OldFeeConfig abiFeeConfig
	NewFeeConfig abiFeeConfig
}

// PackFeeConfigUpdatedEvent returns topic hashes and ABI-encoded non-indexed data.
func PackFeeConfigUpdatedEvent(sender common.Address, oldConfig commontype.ACP224FeeConfig, newConfig commontype.ACP224FeeConfig) ([]common.Hash, []byte, error) {
	return ACP224FeeManagerABI.PackEvent("FeeConfigUpdated", sender, toABIFeeConfig(oldConfig), toABIFeeConfig(newConfig))
}

// UnpackFeeConfigUpdatedEventData decodes the non-indexed portion of a FeeConfigUpdated log.
func UnpackFeeConfigUpdatedEventData(dataBytes []byte) (FeeConfigUpdatedEventData, error) {
	abiData := abiFeeConfigUpdatedEventData{}
	err := ACP224FeeManagerABI.UnpackIntoInterface(&abiData, "FeeConfigUpdated", dataBytes)
	if err != nil {
		return FeeConfigUpdatedEventData{}, err
	}
	oldConfig, err := fromABIFeeConfig(abiData.OldFeeConfig)
	if err != nil {
		return FeeConfigUpdatedEventData{}, err
	}
	newConfig, err := fromABIFeeConfig(abiData.NewFeeConfig)
	if err != nil {
		return FeeConfigUpdatedEventData{}, err
	}
	return FeeConfigUpdatedEventData{
		OldFeeConfig: oldConfig,
		NewFeeConfig: newConfig,
	}, nil
}
