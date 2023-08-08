// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"time"

	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/tests/fixture/testnet"
)

const (
	// Constants defining the names of shell variables whose value can
	// configure local network orchestration.
	AvalancheGoPathEnvName = "AVALANCHEGO_PATH"
	NetworkDirEnvName      = "TESTNETCTL_NETWORK_DIR"
	RootDirEnvName         = "TESTNETCTL_ROOT_DIR"

	DefaultNetworkStartTimeout = 2 * time.Minute
	DefaultNodeInitTimeout     = 10 * time.Second
	DefaultNodeStopTimeout     = 5 * time.Second
	DefaultNodeTickerInterval  = 50 * time.Millisecond
)

// A set of flags appropriate for local testing.
func LocalFlags() testnet.FlagsMap {
	// Supply only non-default configuration to ensure that default values will be used.
	return testnet.FlagsMap{
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
	}
}

// C-Chain config for local testing.
func LocalCChainConfig() testnet.FlagsMap {
	// Supply only non-default configuration to ensure that default
	// values will be used. Available C-Chain configuration options are
	// defined in the `github.com/ava-labs/coreth/evm` package.
	return testnet.FlagsMap{
		"log-level": "debug",
	}
}
