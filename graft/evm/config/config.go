// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package config provides shared configuration types for EVM-based chains in AvalancheGo.
//
//   - CommonConfig: Base configuration shared by all EVM chains (pruning, gossip, APIs, etc.)
//   - CChainConfig: C-Chain specific configuration (embeds CommonConfig, adds price options)
//   - L1Config: L1/Subnet-EVM specific configuration (embeds CommonConfig, adds validators API, database settings)
package config

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	ErrPopulateMissingTriesWithPruning  = errors.New("cannot enable populate missing tries while pruning is enabled")
	ErrPopulateMissingTriesNoReader     = errors.New("cannot enable populate missing tries without at least one reader")
	ErrOfflinePruningWithoutPruning     = errors.New("cannot run offline pruning while pruning is disabled")
	ErrPruningZeroCommitInterval        = errors.New("cannot use commit interval of 0 with pruning enabled")
	ErrPruningZeroStateHistory          = errors.New("cannot use state history of 0 with pruning enabled")
	ErrPushGossipPercentStakeOutOfRange = errors.New("push-gossip-percent-stake must be in the range [0, 1]")
)

// Configurable is the constraint for config types that can be validated and deprecated.
type Configurable[T any] interface {
	*T
	validate(networkID uint32) error
	deprecate() string
}

// GetConfig returns a new config object with the default values set and the
// deprecation message.
// If configBytes is not empty, it will be unmarshalled into the config object.
// If the unmarshalling fails, an error is returned.
// If the config is invalid, an error is returned.
func GetConfig[T any, PT Configurable[T]](configBytes []byte, networkID uint32, newDefault func() T) (T, string, error) {
	config := newDefault()
	if len(configBytes) > 0 {
		if err := json.Unmarshal(configBytes, &config); err != nil {
			var zero T
			return zero, "", fmt.Errorf("failed to unmarshal config %s: %w", string(configBytes), err)
		}
	}
	ptr := PT(&config)
	if err := ptr.validate(networkID); err != nil {
		var zero T
		return zero, "", err
	}
	// We should deprecate config flags as the first thing, before we do anything else
	// because this can set old flags to new flags. log the message after we have
	// initialized the logger.
	deprecateMsg := ptr.deprecate()
	return config, deprecateMsg, nil
}

// deprecate returns a string of deprecation messages for the config.
// This is used to log a message when the config is loaded and contains deprecated flags.
// This function should be kept as a placeholder even if it is empty.
func (*CommonConfig) deprecate() string {
	return ""
}

// validateCommon checks common config fields shared between CChainConfig and L1Config.
func (c *CommonConfig) validateCommon() error {
	if c.PopulateMissingTries != nil && (c.OfflinePruning || c.Pruning) {
		return ErrPopulateMissingTriesWithPruning
	}
	if c.PopulateMissingTries != nil && c.PopulateMissingTriesParallelism < 1 {
		return ErrPopulateMissingTriesNoReader
	}

	if !c.Pruning && c.OfflinePruning {
		return ErrOfflinePruningWithoutPruning
	}
	// If pruning is enabled, the commit interval must be non-zero so the node commits state tries every CommitInterval blocks.
	if c.Pruning && c.CommitInterval == 0 {
		return ErrPruningZeroCommitInterval
	}
	if c.Pruning && c.StateHistory == 0 {
		return ErrPruningZeroStateHistory
	}

	if c.PushGossipPercentStake < 0 || c.PushGossipPercentStake > 1 {
		return fmt.Errorf("%w, got %f", ErrPushGossipPercentStakeOutOfRange, c.PushGossipPercentStake)
	}
	return nil
}

// validate checks the CChainConfig for invalid settings.
func (c *CChainConfig) validate(networkID uint32) error {
	// Ensure that non-standard commit interval is not allowed for production networks
	if constants.ProductionNetworkIDs.Contains(networkID) {
		defaultConfig := NewDefaultCChainConfig()
		if c.CommitInterval != defaultConfig.CommitInterval {
			return fmt.Errorf("cannot start non-local network with commit interval %d different than %d", c.CommitInterval, defaultConfig.CommitInterval)
		}
		if c.StateSyncCommitInterval != defaultConfig.StateSyncCommitInterval {
			return fmt.Errorf("cannot start non-local network with syncable interval %d different than %d", c.StateSyncCommitInterval, defaultConfig.StateSyncCommitInterval)
		}
	}

	return c.CommonConfig.validateCommon()
}

// validate checks the L1Config for invalid settings.
func (c *L1Config) validate(_ uint32) error {
	return c.CommonConfig.validateCommon()
}
