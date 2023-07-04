// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	blockCacheSize               = 64 * units.MiB
	txCacheSize                  = 128 * units.MiB
	transformedSubnetTxCacheSize = 4 * units.MiB

	validatorDiffsCacheSize = 2048
	rewardUTXOsCacheSize    = 2048
	chainCacheSize          = 2048
	chainDBCacheSize        = 2048
)

// ExecutionConfig provides execution parameters of PlatformVM
type ExecutionConfig struct {
	BlockCacheSize               int `json:"blockCacheSize"`
	TxCacheSize                  int `json:"txCacheSize"`
	TransformedSubnetTxCacheSize int `json:"transformedSubnetTxCacheSize"`
	ValidatorDiffsCacheSize      int `json:"validatorDiffsCacheSize"`
	RewardUTXOsCacheSize         int `json:"rewardUTXOsCacheSize"`
	ChainCacheSize               int `json:"chainCacheSize"`
	ChainDBCacheSize             int `json:"chainDBCacheSize"`
}

// GetExecutionConfig returns an ExecutionConfig
// input is unmarshalled into an ExecutionConfig previously
// initialized with default values
func GetExecutionConfig(b []byte) (*ExecutionConfig, error) {
	ec := &ExecutionConfig{
		BlockCacheSize:               blockCacheSize,
		TxCacheSize:                  txCacheSize,
		TransformedSubnetTxCacheSize: transformedSubnetTxCacheSize,
		ValidatorDiffsCacheSize:      validatorDiffsCacheSize,
		RewardUTXOsCacheSize:         rewardUTXOsCacheSize,
		ChainCacheSize:               chainCacheSize,
		ChainDBCacheSize:             chainDBCacheSize,
	}

	// if bytes are empty keep default values
	if len(b) == 0 {
		return ec, nil
	}

	err := json.Unmarshal(b, ec)
	if err != nil {
		return nil, err
	}
	return ec, nil
}
