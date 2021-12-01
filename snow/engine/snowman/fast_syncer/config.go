// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package snowsyncer

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
)

type Config struct {
	common.Config
	StateSyncTestingBeacons []ids.ShortID // testing beacons from which nodes fast sync without network consensus
	VM                      block.ChainVM
	Starter                 common.GearStarter
}
