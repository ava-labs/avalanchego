// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package config

import (
	"encoding/json"
	"fmt"

	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/graft/evm/config"
)

// Config contains configuration for Subnet-EVM (L1).
type Config struct {
	config.BaseConfig

	AirdropFile string `json:"airdrop"`

	ValidatorsAPIEnabled bool `json:"validators-api-enabled"`

	// Addresses that should be prioritized for regossip
	PriorityRegossipAddresses []common.Address `json:"priority-regossip-addresses"`

	// Address for transaction fees (must be empty if not supported by blockchain)
	FeeRecipient string `json:"feeRecipient"`

	// Database settings
	UseStandaloneDatabase *bool  `json:"use-standalone-database"`
	DatabaseConfigContent string `json:"database-config"`
	DatabaseConfigFile    string `json:"database-config-file"`
	DatabaseType          string `json:"database-type"`
	DatabasePath          string `json:"database-path"`
	DatabaseReadOnly      bool   `json:"database-read-only"`
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
func (c *Config) Validate(_ uint32) error {
	return c.BaseConfig.ValidateBase()
}

// Deprecate returns a string of deprecation messages for Config.
func (c *Config) Deprecate() string {
	return ""
}
