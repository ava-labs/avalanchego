// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"time"

	_ "embed"
)

const (
	Client = "avalanchego"
	// RPCChainVMProtocol should be bumped anytime changes are made which
	// require the plugin vm to upgrade to latest avalanchego release to be
	// compatible.
	RPCChainVMProtocol uint = 44

	CurrentDatabase = "v1.4.5"
	PrevDatabase    = "v1.0.0"
)

// These are globals that describe network upgrades and node versions
var (
	Current = &Application{
		Name:  Client,
		Major: 1,
		Minor: 14,
		Patch: 1,
	}
	MinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 14,
		Patch: 0,
	}
	PrevMinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 13,
		Patch: 0,
	}

	//go:embed compatibility.json
	rpcChainVMProtocolCompatibilityBytes []byte
	// RPCChainVMProtocolCompatibility maps RPCChainVMProtocol versions to the
	// set of avalanchego versions that supported that version. This is not used
	// by avalanchego, but is useful for downstream libraries.
	RPCChainVMProtocolCompatibility map[uint][]string
)

func init() {
	err := json.Unmarshal(rpcChainVMProtocolCompatibilityBytes, &RPCChainVMProtocolCompatibility)
	if err != nil {
		panic(err)
	}
}

func GetCompatibility(upgradeTime time.Time) *Compatibility {
	return &Compatibility{
		Current:                   Current,
		MinCompatibleAfterUpgrade: MinimumCompatibleVersion,
		MinCompatible:             PrevMinimumCompatibleVersion,
		UpgradeTime:               upgradeTime,
	}
}
