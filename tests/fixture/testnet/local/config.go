// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package local

import (
	"time"

	cfg "github.com/ava-labs/avalanchego/config"
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

	// The default initial static port should differ from the AvalancheGo default of 9650.
	DefaultInitialStaticPort = 9850
)

// A set of flags appropriate for local testing.
func LocalFlags() testnet.FlagsMap {
	// Supply only non-default configuration to ensure that default values will be used.
	return testnet.FlagsMap{
		cfg.NetworkPeerListGossipFreqKey: "250ms",
		cfg.NetworkMaxReconnectDelayKey:  "1s",
		cfg.PublicIPKey:                  "127.0.0.1",
		cfg.HTTPHostKey:                  "127.0.0.1",
		cfg.StakingHostKey:               "127.0.0.1",
		cfg.HealthCheckFreqKey:           "2s",
		cfg.AdminAPIEnabledKey:           true,
		cfg.IpcAPIEnabledKey:             true,
		cfg.IndexEnabledKey:              true,
		cfg.LogDisplayLevelKey:           "INFO",
		cfg.LogLevelKey:                  "DEBUG",
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
