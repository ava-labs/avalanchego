// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"time"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/utils/logging"
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

// Flags appropriate for networks used for all types of testing.
func DefaultTestFlags() FlagsMap {
	return FlagsMap{
		config.NetworkPeerListPullGossipFreqKey: "250ms",
		config.NetworkMaxReconnectDelayKey:      "1s",
		config.HealthCheckFreqKey:               "500ms",
		config.AdminAPIEnabledKey:               true,
		config.IndexEnabledKey:                  true,
	}
}

// Flags appropriate for a node running as a local process
func DefaultProcessFlags() FlagsMap {
	return FlagsMap{
		config.PublicIPKey:        "127.0.0.1",
		config.HTTPHostKey:        "127.0.0.1",
		config.StakingHostKey:     "127.0.0.1",
		config.LogDisplayLevelKey: logging.Off.String(), // Display logging not needed since nodes run headless
		config.LogLevelKey:        logging.Debug.String(),
		// Default to dynamic port allocation
		config.HTTPPortKey:    0,
		config.StakingPortKey: 0,
	}
}

// Flags appropriate for a node running as a local process
func DefaultKubeFlags(includePluginDir bool) FlagsMap {
	flags := FlagsMap{
		config.HTTPHostKey:        "0.0.0.0", // Need to bind to pod IP to ensure kubelet can access the http port for readiness check
		config.LogDisplayLevelKey: logging.Info.String(),
		// TODO(marun) Revert
		config.LogLevelKey: logging.Info.String(), // Assume collection of stdout logs
	}
	if includePluginDir {
		flags[config.PluginDirKey] = "/avalanchego/build/plugins"
	}
	return flags
}

// Flags required by e2e testing
func DefaultE2EFlags() FlagsMap {
	return FlagsMap{
		config.MinStakeDurationKey:           DefaultMinStakeDuration.String(),
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
