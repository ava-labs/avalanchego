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

// PackFeeConfigUpdatedEvent returns topic hashes and ABI-encoded non-indexed data.
func PackFeeConfigUpdatedEvent(sender common.Address, oldConfig commontype.ACP224FeeConfig, newConfig commontype.ACP224FeeConfig) ([]common.Hash, []byte, error) {
	return ACP224FeeManagerABI.PackEvent("FeeConfigUpdated", sender, oldConfig, newConfig)
}

// UnpackFeeConfigUpdatedEventData decodes the non-indexed portion of a FeeConfigUpdated log.
func UnpackFeeConfigUpdatedEventData(dataBytes []byte) (FeeConfigUpdatedEventData, error) {
	var data FeeConfigUpdatedEventData
	err := ACP224FeeManagerABI.UnpackIntoInterface(&data, "FeeConfigUpdated", dataBytes)
	return data, err
}
