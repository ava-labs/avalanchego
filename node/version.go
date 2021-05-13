// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

var (
	Version                      = version.NewDefaultApplication(constants.PlatformName, 1, 4, 4)
	MinimumCompatibleVersion     = version.NewDefaultApplication(constants.PlatformName, 1, 4, 0)
	PrevMinimumCompatibleVersion = version.NewDefaultApplication(constants.PlatformName, 1, 3, 0)
	MinimumUnmaskedVersion       = version.NewDefaultApplication(constants.PlatformName, 1, 1, 0)
	PrevMinimumUnmaskedVersion   = version.NewDefaultApplication(constants.PlatformName, 1, 0, 0)
	VersionParser                = version.NewDefaultApplicationParser()

	DatabaseVersion     = version.NewDefaultVersion(1, 4, 3)
	PrevDatabaseVersion = version.NewDefaultVersion(1, 0, 0)

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
