// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/avalanchego/graft/evm/config"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

const (
	defaultCommitInterval = 4096
)

// Config contains configuration for the C-Chain.
type Config struct {
	config.BaseConfig

	// GasTarget is the target gas per second that this node will attempt to use
	// when creating blocks. If not specified, the node will default to use the
	// parent block's target gas per second.
	GasTarget *gas.Gas `json:"gas-target,omitempty"`

	// Price option settings for transaction fee estimation
	PriceOptionSlowFeePercentage uint64 `json:"price-options-slow-fee-percentage"`
	PriceOptionFastFeePercentage uint64 `json:"price-options-fast-fee-percentage"`
	PriceOptionMaxTip            uint64 `json:"price-options-max-tip"`
}

// GetConfig returns a config object with default values applied and the deprecation message.
// Defaults are applied first, then user-provided config values override them.
func GetConfig(configBytes []byte, networkID uint32) (Config, string, error) {
	var cfg Config
	cfg.setDefaults()

	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &cfg); err != nil {
			return Config{}, "", fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}

	if err := cfg.Validate(networkID); err != nil {
		return Config{}, "", err
	}

	deprecateMsg := cfg.Deprecate()
	return cfg, deprecateMsg, nil
}

// Validate checks the Config for invalid settings.
func (c *Config) Validate(networkID uint32) error {
	// Ensure that non-standard commit interval is not allowed for production networks
	if constants.ProductionNetworkIDs.Contains(networkID) {
		if c.CommitInterval != defaultCommitInterval {
			return fmt.Errorf("cannot start non-local network with commit interval %d different than %d", c.CommitInterval, defaultCommitInterval)
		}
		if c.StateSyncCommitInterval != defaultCommitInterval*4 {
			return fmt.Errorf("cannot start non-local network with syncable interval %d different than %d", c.StateSyncCommitInterval, defaultCommitInterval*4)
		}
	}

	return c.BaseConfig.ValidateBase()
}

// Deprecate returns a string of deprecation messages for Config.
func (c *Config) Deprecate() string {
	return ""
}
