// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowsyncer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/tracker"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Config struct {
	common.Config
	common.AllGetsServer
	StateSyncTestingBeacons []ids.ShortID // testing beacons from which nodes fast sync without network consensus
	VM                      block.ChainVM
	WeightTracker           tracker.WeightTracker
}
