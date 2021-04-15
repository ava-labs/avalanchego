// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package versionconfig

import (
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

var (
	NodeVersion             = version.NewDefaultApplication(constants.AppName, 1, 3, 3)
	PreDBUpgradeNodeVersion = version.NewDefaultApplication(constants.AppName, 1, 3, 1)

	CurrentDBVersion = version.NewDefaultVersion(1, 1, 0)
	PrevDBVersion    = version.NewDefaultVersion(1, 0, 0)
)
