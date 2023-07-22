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
	checksumsEnabled        = false
)

// ExecutionConfig provides execution parameters of PlatformVM
type ExecutionConfig struct {
	BlockCacheSize               int  `json:"block-cache-size"`
	TxCacheSize                  int  `json:"tx-cache-size"`
	TransformedSubnetTxCacheSize int  `json:"transformed-subnet-tx-cache-size"`
	ValidatorDiffsCacheSize      int  `json:"validator-diffs-cache-size"`
	RewardUTXOsCacheSize         int  `json:"reward-utxos-cache-size"`
	ChainCacheSize               int  `json:"chain-cache-size"`
	ChainDBCacheSize             int  `json:"chain-db-cache-size"`
	ChecksumsEnabled             bool `json:"checksums-enabled"`
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
		ChecksumsEnabled:             checksumsEnabled,
	}

	// if bytes are empty keep default values
	if len(b) == 0 {
		return ec, nil
	}

	return ec, json.Unmarshal(b, ec)
}
