// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/database/memdb"
	"github.com/ava-labs/gecko/snow/consensus/avalanche"
	"github.com/ava-labs/gecko/snow/consensus/snowball"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/common/queue"
)

func DefaultConfig() Config {
	vtxBlocked, _ := queue.New(memdb.New())
	txBlocked, _ := queue.New(memdb.New())
	return Config{
		BootstrapConfig: BootstrapConfig{
			Config:     common.DefaultConfigTest(),
			VtxBlocked: vtxBlocked,
			TxBlocked:  txBlocked,
			State:      &stateTest{},
			VM:         &VMTest{},
		},
		Params: avalanche.Parameters{
			Parameters: snowball.Parameters{
				Metrics:              prometheus.NewRegistry(),
				K:                    1,
				Alpha:                1,
				BetaVirtuous:         1,
				BetaRogue:            2,
				ConcurrentRepolls:    1,
			},
			Parents:   2,
			BatchSize: 1,
		},
		Consensus: &avalanche.Topological{},
	}
}
