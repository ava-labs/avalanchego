// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils/constants"
)

const (
	Client = "avalanchego"
	// RPCChainVMProtocol should be bumped anytime changes are made which
	// require the plugin vm to upgrade to latest avalanchego release to be
	// compatible.
	RPCChainVMProtocol uint = 36
)

// These are globals that describe network upgrades and node versions
var (
	Current = &Semantic{
		Major: 1,
		Minor: 11,
		Patch: 10,
	}
	CurrentApp = &Application{
		Name:  Client,
		Major: Current.Major,
		Minor: Current.Minor,
		Patch: Current.Patch,
	}
	MinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 11,
		Patch: 0,
	}
	PrevMinimumCompatibleVersion = &Application{
		Name:  Client,
		Major: 1,
		Minor: 10,
		Patch: 0,
	}

	CurrentDatabase = DatabaseVersion1_4_5
	PrevDatabase    = DatabaseVersion1_0_0

	DatabaseVersion1_4_5 = &Semantic{
		Major: 1,
		Minor: 4,
		Patch: 5,
	}
	DatabaseVersion1_0_0 = &Semantic{
		Major: 1,
		Minor: 0,
		Patch: 0,
	}

	//go:embed compatibility.json
	rpcChainVMProtocolCompatibilityBytes []byte
	// RPCChainVMProtocolCompatibility maps RPCChainVMProtocol versions to the
	// set of avalanchego versions that supported that version. This is not used
	// by avalanchego, but is useful for downstream libraries.
	RPCChainVMProtocolCompatibility map[uint][]*Semantic

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase1Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase1Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase1Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase2Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase2Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase2Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase3Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase3Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase3Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase4Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase4Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase4Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase5Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase5Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase5Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhasePre6Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhasePre6Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhasePre6Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhase6Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhase6Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhase6Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	ApricotPhasePost6Times = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.ApricotPhasePost6Time,
		constants.FujiID:    upgrade.Fuji.ApricotPhasePost6Time,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	BanffTimes = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.BanffTime,
		constants.FujiID:    upgrade.Fuji.BanffTime,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	CortinaTimes = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.CortinaTime,
		constants.FujiID:    upgrade.Fuji.CortinaTime,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	DurangoTimes = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.DurangoTime,
		constants.FujiID:    upgrade.Fuji.DurangoTime,
	}

	// Deprecated: This will be removed once coreth no longer uses it.
	EUpgradeTimes = map[uint32]time.Time{
		constants.MainnetID: upgrade.Mainnet.EtnaTime,
		constants.FujiID:    upgrade.Fuji.EtnaTime,
	}
)

func init() {
	var parsedRPCChainVMCompatibility map[uint][]string
	err := json.Unmarshal(rpcChainVMProtocolCompatibilityBytes, &parsedRPCChainVMCompatibility)
	if err != nil {
		panic(err)
	}

	RPCChainVMProtocolCompatibility = make(map[uint][]*Semantic)
	for rpcChainVMProtocol, versionStrings := range parsedRPCChainVMCompatibility {
		versions := make([]*Semantic, len(versionStrings))
		for i, versionString := range versionStrings {
			version, err := Parse(versionString)
			if err != nil {
				panic(err)
			}
			versions[i] = version
		}
		RPCChainVMProtocolCompatibility[rpcChainVMProtocol] = versions
	}
}

func GetCompatibility(minCompatibleTime time.Time) Compatibility {
	return NewCompatibility(
		CurrentApp,
		MinimumCompatibleVersion,
		minCompatibleTime,
		PrevMinimumCompatibleVersion,
	)
}
