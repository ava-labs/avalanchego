// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package config provides shared base configuration for EVM-based chains in AvalancheGo.
//
// BaseConfig contains fields shared by both C-Chain and Subnet-EVM implementations,
// including pruning, gossip, APIs, caching, and transaction pool settings.
//
// Implementation-specific config packages:
//   - graft/coreth/plugin/evm/config - C-Chain configuration
//   - graft/subnet-evm/plugin/evm/config - Subnet-EVM (L1) configuration
package config

import (
	"errors"
	"fmt"
)

var (
	ErrPopulateMissingTriesWithPruning  = errors.New("cannot enable populate missing tries while pruning is enabled")
	ErrPopulateMissingTriesNoReader     = errors.New("cannot enable populate missing tries without at least one reader")
	ErrOfflinePruningWithoutPruning     = errors.New("cannot run offline pruning while pruning is disabled")
	ErrPruningZeroCommitInterval        = errors.New("cannot use commit interval of 0 with pruning enabled")
	ErrPruningZeroStateHistory          = errors.New("cannot use state history of 0 with pruning enabled")
	ErrPushGossipPercentStakeOutOfRange = errors.New("push-gossip-percent-stake must be in the range [0, 1]")
)

// ValidateBase checks base config fields.
func (c *BaseConfig) ValidateBase() error {
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
