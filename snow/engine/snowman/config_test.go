// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
)

func DefaultConfig() Config {
	blocked, _ := queue.New(memdb.New())
	return Config{
		BootstrapConfig: BootstrapConfig{
			Config:  common.DefaultConfigTest(),
			Blocked: blocked,
			VM:      &TestVM{},
		},
		Params: snowball.Parameters{
			Metrics:           prometheus.NewRegistry(),
			K:                 1,
			Alpha:             1,
			BetaVirtuous:      1,
			BetaRogue:         2,
			ConcurrentRepolls: 1,
		},
		Consensus: &snowman.Topological{},
	}
}
