// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"time"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

const (
	// Interval appropriate for network operations that should be
	// retried periodically but not too often.
	DefaultPollingInterval = 500 * time.Millisecond

	// Validator start time must be a minimum of SyncBound from the
	// current time for validator addition to succeed, and adding 20
	// seconds provides a buffer in case of any delay in processing.
	DefaultValidatorStartTimeDiff = executor.SyncBound + 20*time.Second

	DefaultNetworkTimeout = 2 * time.Minute

	// Minimum required to ensure connectivity-based health checks will pass
	DefaultNodeCount = 2

	// Arbitrary number of pre-funded keys to create by default
	DefaultPreFundedKeyCount = 50

	// A short minimum stake duration enables testing of staking logic.
	DefaultMinStakeDuration = time.Second

	defaultConfigFilename = "config.json"
)

// A set of flags appropriate for testing.
func DefaultFlags() FlagsMap {
	// Supply only non-default configuration to ensure that default values will be used.
	return FlagsMap{
		config.NetworkPeerListGossipFreqKey: "250ms",
		config.NetworkMaxReconnectDelayKey:  "1s",
		config.PublicIPKey:                  "127.0.0.1",
		config.HTTPHostKey:                  "127.0.0.1",
		config.StakingHostKey:               "127.0.0.1",
		config.HealthCheckFreqKey:           "2s",
		config.AdminAPIEnabledKey:           true,
		config.IpcAPIEnabledKey:             true,
		config.IndexEnabledKey:              true,
		config.LogDisplayLevelKey:           "INFO",
		config.LogLevelKey:                  "DEBUG",
		config.MinStakeDurationKey:          DefaultMinStakeDuration.String(),
	}
}

// A set of chain configurations appropriate for testing.
func DefaultChainConfigs() map[string]FlagsMap {
	return map[string]FlagsMap{
		// Supply only non-default configuration to ensure that default
		// values will be used. Available C-Chain configuration options are
		// defined in the `github.com/ava-labs/coreth/evm` package.
		"C": {
			"warp-api-enabled": true,
			"log-level":        "trace",
		},
	}
}
