// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"time"

	"github.com/chain4travel/caminogo/utils/constants"
)

// These are globals that describe network upgrades and node versions
var (
	// GitCommit will be set with -X during build step
	GitCommit        string
	ModuleVersion    = NewDefaultVersion(0, 0, 2)
	ModuleVersionApp = NewDefaultApplication(
		constants.PlatformName,
		ModuleVersion.Major(),
		ModuleVersion.Minor(),
		ModuleVersion.Patch(),
	)

	CurrentDatabase = DatabaseVersion1_4_5
	PrevDatabase    = DatabaseVersion1_0_0

	DatabaseVersion1_4_5 = NewDefaultVersion(1, 4, 5)
	DatabaseVersion1_0_0 = NewDefaultVersion(1, 0, 0)

	ApricotPhase0Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2020, time.December, 8, 3, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC),
	}
	ApricotPhase0DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase1Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.March, 31, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.March, 26, 14, 0, 0, 0, time.UTC),
	}
	ApricotPhase1DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase2Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.May, 10, 11, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.May, 5, 14, 0, 0, 0, time.UTC),
	}
	ApricotPhase2DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase3Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.August, 24, 14, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.August, 16, 19, 0, 0, 0, time.UTC),
	}
	ApricotPhase3DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	ApricotPhase4Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.September, 22, 21, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.September, 16, 21, 0, 0, 0, time.UTC),
	}
	ApricotPhase4DefaultTime     = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)
	ApricotPhase4MinPChainHeight = map[uint32]uint64{
		constants.MainnetID: 793005,
		constants.FujiID:    47437,
	}
	ApricotPhase4DefaultMinPChainHeight uint64

	ApricotPhase5Times = map[uint32]time.Time{
		constants.MainnetID: time.Date(2021, time.December, 2, 18, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(2021, time.November, 24, 15, 0, 0, 0, time.UTC),
	}
	ApricotPhase5DefaultTime = time.Date(2020, time.December, 5, 5, 0, 0, 0, time.UTC)

	SunrisePhase0Times       = map[uint32]time.Time{}
	SunrisePhase0DefaultTime = time.Date(2022, time.May, 16, 8, 0, 0, 0, time.UTC)

	// FIXME: update this before release
	XChainMigrationTimes = map[uint32]time.Time{
		constants.MainnetID: time.Date(10000, time.December, 1, 0, 0, 0, 0, time.UTC),
		constants.FujiID:    time.Date(10000, time.December, 1, 0, 0, 0, 0, time.UTC),
	}
	XChainMigrationDefaultTime = time.Date(2022, time.January, 1, 1, 0, 0, 0, time.UTC)
)

func GetApricotPhase0Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase0Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase0DefaultTime
}

func GetApricotPhase1Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase1Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase1DefaultTime
}

func GetApricotPhase2Time(networkID uint32) time.Time {
	if upgradeTime, exists := ApricotPhase2Times[networkID]; exists {
		return upgradeTime
	}
	return ApricotPhase2DefaultTime
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
	return SunrisePhase0DefaultTime
}

func GetXChainMigrationTime(networkID uint32) time.Time {
	if upgradeTime, exists := XChainMigrationTimes[networkID]; exists {
		return upgradeTime
	}
	return XChainMigrationDefaultTime
}

func GetCompatibility(networkID uint32) Compatibility {
	return NewCompatibility(
		ModuleVersionApp,
		ModuleVersionApp,
		GetSunrisePhase0Time(networkID),
		ModuleVersionApp,
		ModuleVersionApp,
		GetApricotPhase0Time(networkID),
		ModuleVersionApp,
	)
}
