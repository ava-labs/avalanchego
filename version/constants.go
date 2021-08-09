// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package version

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	String                       string // Printed when CLI arg --version is used
	GitCommit                    string // Set in the build script (i.e. at compile time)
	Current                      = NewDefaultVersion(1, 4, 12)
	CurrentApp                   = NewDefaultApplication(constants.PlatformName, Current.Major(), Current.Minor(), Current.Patch())
	MinimumCompatibleVersion     = NewDefaultApplication(constants.PlatformName, 1, 4, 5)
	PrevMinimumCompatibleVersion = NewDefaultApplication(constants.PlatformName, 1, 3, 0)
	MinimumUnmaskedVersion       = NewDefaultApplication(constants.PlatformName, 1, 1, 0)
	PrevMinimumUnmaskedVersion   = NewDefaultApplication(constants.PlatformName, 1, 0, 0)
	VersionParser                = NewDefaultApplicationParser()

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
)

func init() {
	format := "%s [database=%s"
	args := []interface{}{
		CurrentApp,
		CurrentDatabase,
	}
	if GitCommit != "" {
		format += ", commit=%s"
		args = append(args, GitCommit)
	}
	format += "]\n"
	String = fmt.Sprintf(format, args...)
}

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

func GetCompatibility(networkID uint32) Compatibility {
	return NewCompatibility(
		CurrentApp,
		MinimumCompatibleVersion,
		GetApricotPhase2Time(networkID),
		PrevMinimumCompatibleVersion,
		MinimumUnmaskedVersion,
		GetApricotPhase0Time(networkID),
		PrevMinimumUnmaskedVersion,
	)
}
