// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************
// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"encoding/json"
	"time"

	_ "embed"

	"github.com/ava-labs/avalanchego/utils/constants"
)

// RPCChainVMProtocol should be bumped anytime changes are made which require
// the plugin vm to upgrade to latest avalanchego release to be compatible.
const RPCChainVMProtocol uint = 25

// These are globals that describe network upgrades and node versions
var (
	// GitCommit and GitVersion will be set with -X during build step
	GitCommit  string = "unknown"
	GitVersion string = "unknown"

	Current = &Semantic{
		Major: 1,
		Minor: 1,
		Patch: 0,
	}
	CurrentApp = &Application{
		Major: Current.Major,
		Minor: Current.Minor,
		Patch: Current.Patch,
	}
	MinimumCompatibleVersion = &Application{
		Major: 1,
		Minor: 1,
		Patch: 0,
	}
	PrevMinimumCompatibleVersion = &Application{
		Major: 1,
		Minor: 0,
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

	DefaultUpgradeTime    = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)
	unreachableFutureTime = time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC)

	caminoEarliestTime     = time.Date(2022, time.May, 16, 8, 0, 0, 0, time.UTC)
	columbusEarliestTime   = time.Date(2022, time.May, 16, 8, 0, 0, 0, time.UTC)
	kopernikusEarliestTime = time.Date(2022, time.May, 16, 8, 0, 0, 0, time.UTC)

	ApricotPhase1Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.March, 26, 14, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}

	ApricotPhase2Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.May, 10, 11, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.May, 5, 14, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}

	ApricotPhase3Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.August, 24, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.August, 16, 19, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}
	ApricotPhase3DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase4Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.September, 22, 21, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.September, 16, 21, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}
	ApricotPhase4DefaultTime     = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)
	ApricotPhase4MinPChainHeight = map[uint32]uint64{
		constants.MainnetID: 793005,
		constants.FujiID:    47437,

		constants.CaminoID:     0,
		constants.ColumbusID:   0,
		constants.KopernikusID: 0,
	}
	ApricotPhase4DefaultMinPChainHeight uint64

	ApricotPhase5Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.December, 2, 18, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.November, 24, 15, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}
	ApricotPhase5DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhasePre6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 5, 1, 30, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}

	SunrisePhase0Times = map[uint32]time.Time{
		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}

	ApricotPhase6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 6, 20, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}

	ApricotPhasePost6Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.September, 7, 3, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.September, 7, 6, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}
	ApricotPhase6DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	BanffTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(2022, time.October, 18, 16, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2022, time.October, 3, 14, 0, 0, 0, time.UTC),

		constants.CaminoID:     caminoEarliestTime,
		constants.ColumbusID:   columbusEarliestTime,
		constants.KopernikusID: kopernikusEarliestTime,
	}
	BanffDefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	AthensPhaseTimes = map[uint32]time.Time{
		constants.KopernikusID: time.Date(2023, time.July, 4, 13, 0, 0, 0, time.UTC),
		constants.ColumbusID:   time.Date(2023, time.July, 7, 8, 0, 0, 0, time.UTC),
		constants.CaminoID:     time.Date(2023, time.July, 17, 8, 0, 0, 0, time.UTC),
	}

	// TODO @evlekht update this before release
	BerlinPhaseTimes = map[uint32]time.Time{
		constants.KopernikusID: unreachableFutureTime,
		constants.ColumbusID:   unreachableFutureTime,
		constants.CaminoID:     unreachableFutureTime,
	}

	// TODO @evlekht update this before release, must be exactly the same as Berlin
	CortinaTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(2023, time.April, 25, 15, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2023, time.April, 6, 15, 0, 0, 0, time.UTC),

		constants.KopernikusID: unreachableFutureTime,
		constants.ColumbusID:   unreachableFutureTime,
		constants.CaminoID:     unreachableFutureTime,
	}
	CortinaDefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)
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

func GetApricotPhase3Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase3Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase3DefaultTime
}

func GetApricotPhase4Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase4Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase4DefaultTime
}

func GetApricotPhase4MinPChainHeight(networkID uint32) uint64 {
	if minHeight, exists := ApricotPhase4MinPChainHeight[networkID]; exists {
		return minHeight
	}
	return ApricotPhase4DefaultMinPChainHeight
}

func GetApricotPhase5Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase5Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase5DefaultTime
}

func GetSunrisePhase0Time(networkID uint32) time.Time {
	if upgradeTime, exists := SunrisePhase0Times[networkID]; exists {
		return upgradeTime
	}
	return DefaultUpgradeTime
}

func GetApricotPhase6Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase6Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase6DefaultTime
}

func GetBanffTime(networkID uint32) time.Time {
	if upgradeTime, exists := BanffTimes[networkID]; exists {
		return upgradeTime
	}
	return BanffDefaultTime
}

func GetAthensPhaseTime(networkID uint32) time.Time {
	if upgradeTime, exists := AthensPhaseTimes[networkID]; exists {
		return upgradeTime
	}
	return DefaultUpgradeTime
}

func GetBerlinPhaseTime(networkID uint32) time.Time {
	if upgradeTime, exists := BerlinPhaseTimes[networkID]; exists {
		return upgradeTime
	}
	return DefaultUpgradeTime
}

func GetCortinaTime(networkID uint32) time.Time {
	if upgradeTime, exists := CortinaTimes[networkID]; exists {
		return upgradeTime
	}
	return CortinaDefaultTime
}

func GetCompatibility(networkID uint32) Compatibility {
	return NewCompatibility(
		CurrentApp,
		MinimumCompatibleVersion,
		GetBerlinPhaseTime(networkID).Add(-time.Minute),
		PrevMinimumCompatibleVersion,
	)
}
