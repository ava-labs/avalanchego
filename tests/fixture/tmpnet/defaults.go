// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tmpnet

import (
	"time"

	"github.com/ava-labs/avalanchego/config"
)

const (
	// Constants defining the names of shell variables whose value can
	// configure temporary network orchestration.
	AvalancheGoPathEnvName = "AVALANCHEGO_PATH"
	NetworkDirEnvName      = "TMPNET_NETWORK_DIR"
	RootDirEnvName         = "TMPNET_ROOT_DIR"

	DefaultNetworkStartTimeout = 2 * time.Minute
	DefaultNodeInitTimeout     = 10 * time.Second
	DefaultNodeStopTimeout     = 5 * time.Second
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

// C-Chain config for testing.
func DefaultCChainConfig() FlagsMap {
	// Supply only non-default configuration to ensure that default
	// values will be used. Available C-Chain configuration options are
	// defined in the `github.com/ava-labs/coreth/evm` package.
	return FlagsMap{
		"log-level": "trace",
	}
}
