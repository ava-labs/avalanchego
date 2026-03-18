// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp224feemanager

import (
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
)

// FeeConfigUpdatedEventData represents a FeeConfigUpdated non-indexed event data raised by the ACP224FeeManager contract.
type FeeConfigUpdatedEventData struct {
	OldFeeConfig commontype.ACP224FeeConfig
	NewFeeConfig commontype.ACP224FeeConfig
}

// abiFeeConfigUpdatedEventData is the ABI-compatible representation using *big.Int fields.
type abiFeeConfigUpdatedEventData struct {
	OldFeeConfig abiFeeConfig
	NewFeeConfig abiFeeConfig
}

// PackFeeConfigUpdatedEvent packs the event into the appropriate arguments for FeeConfigUpdated.
// It returns topic hashes and the encoded non-indexed data.
func PackFeeConfigUpdatedEvent(sender common.Address, oldConfig commontype.ACP224FeeConfig, newConfig commontype.ACP224FeeConfig) ([]common.Hash, []byte, error) {
	return ACP224FeeManagerABI.PackEvent("FeeConfigUpdated", sender, toABIFeeConfig(oldConfig), toABIFeeConfig(newConfig))
}

// UnpackFeeConfigUpdatedEventData attempts to unpack non-indexed [dataBytes].
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
