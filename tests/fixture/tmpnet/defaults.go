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

	// TODO(marun) Vary this between kube and process-based nodes
	// since the timing will be different
	DefaultNetworkTimeout = 4 * time.Minute

	// Minimum required to ensure connectivity-based health checks will pass
	DefaultNodeCount = 2

	// Arbitrary number of pre-funded keys to create by default
	DefaultPreFundedKeyCount = 50

	// A short minimum stake duration enables testing of staking logic.
	DefaultMinStakeDuration = time.Second

	defaultConfigFilename = "config.json"
)

func DefaultTmpnetFlags() FlagsMap {
	return FlagsMap{
		config.NetworkPeerListPullGossipFreqKey: "250ms",
		config.NetworkMaxReconnectDelayKey:      "1s",
		config.HealthCheckFreqKey:               "500ms",
		config.AdminAPIEnabledKey:               true,
		config.IndexEnabledKey:                  true,
	}
}

// Flags required by e2e testing
func DefaultE2EFlags() FlagsMap {
	return FlagsMap{
		config.ProposerVMUseCurrentHeightKey: true,
		// Reducing this from the 1s default speeds up tx acceptance
		config.ProposerVMMinBlockDelayKey: "0s",
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
			// TODO(marun) Revert
			"log-level": "info",
		},
	}
}
